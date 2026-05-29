import Foundation

/// Abstraction over secret persistence so AccountSession is testable without Keychain.
protocol SecretStore {
    func load(_ key: String) -> String?
    func save(_ value: String, for key: String) throws
    @discardableResult func delete(_ key: String) -> Bool
}

/// Keychain-backed SecretStore (scalar service via KeychainService).
struct KeychainSecretStore: SecretStore {
    func load(_ key: String) -> String? { KeychainService.load(key: key) }
    func save(_ value: String, for key: String) throws { try KeychainService.save(key: key, value: value) }
    @discardableResult func delete(_ key: String) -> Bool { KeychainService.delete(key: key) }
}

@MainActor
@Observable
final class AccountSession {
    enum State: Equatable { case loggedOut, loggingIn, loggedIn, sessionExpired }

    private(set) var state: State = .loggedOut
    private(set) var username: String = ""
    private(set) var localDeviceID: String = ""
    private(set) var peers: [RelayPeerDevice] = []
    var lastError: String?

    /// Called after any successful state/peer change so AppState can rebuild targets.
    var onChange: (() -> Void)?

    static let kSessionToken = "tf_relay_session_token"
    static let kDeviceToken = "tf_relay_device_token"
    static let kDeviceID = "tf_relay_device_id"
    static let kUsername = "tf_relay_username"
    static let kDeviceList = "tf_relay_device_list"

    private let client: RelayAccountClienting
    private let secrets: SecretStore
    private let defaults: UserDefaults
    private let hostname: String

    init(client: RelayAccountClienting = RelayAccountClient(),
         secrets: SecretStore = KeychainSecretStore(),
         defaults: UserDefaults = .standard,
         hostname: String = Host.current().localizedName ?? "Mac") {
        self.client = client
        self.secrets = secrets
        self.defaults = defaults
        self.hostname = hostname
    }

    /// Synchronous: set initial state from stored credentials + cached peers.
    /// The caller should kick `refreshDevices()` afterwards.
    func bootstrap() {
        username = defaults.string(forKey: Self.kUsername) ?? ""
        peers = loadCachedPeers()
        localDeviceID = secrets.load(Self.kDeviceID) ?? ""
        if secrets.load(Self.kDeviceToken) != nil && !localDeviceID.isEmpty {
            state = .loggedIn
        } else {
            state = .loggedOut
        }
        notify()
    }

    func login(username u: String, password p: String) async {
        state = .loggingIn; lastError = nil; notify()
        do { try await completeAuth(client.login(username: u, password: p)) }
        catch { fail(error) }
    }

    func register(username u: String, password p: String, inviteCode: String) async {
        state = .loggingIn; lastError = nil; notify()
        do { try await completeAuth(client.register(username: u, password: p, inviteCode: inviteCode)) }
        catch { fail(error) }
    }

    func refreshDevices() async {
        guard let sess = secrets.load(Self.kSessionToken) else {
            state = .sessionExpired; notify(); return
        }
        do {
            let list = try await client.listDevices(session: sess)
            lastError = nil
            peers = list.filter { $0.id != localDeviceID }
            cachePeers(peers)
            if state != .loggedIn { state = .loggedIn }
            notify()
        } catch let RelayAPIError.api(status, code, _) where status == 401 || code == "invalid_session" {
            state = .sessionExpired; notify()
        } catch {
            lastError = error.localizedDescription; notify()
        }
    }

    func logout() {
        secrets.delete(Self.kSessionToken)
        secrets.delete(Self.kDeviceToken)
        secrets.delete(Self.kDeviceID)
        defaults.removeObject(forKey: Self.kUsername)
        defaults.removeObject(forKey: Self.kDeviceList)
        username = ""; localDeviceID = ""; peers = []
        lastError = nil
        state = .loggedOut
        notify()
    }

    func synthesizedTargets() -> [OutputTarget] {
        guard let token = secrets.load(Self.kDeviceToken), !localDeviceID.isEmpty else { return [] }
        return RelayTargetSynthesis.targets(peers: peers, localDeviceID: localDeviceID,
                                            deviceToken: token, relayURL: RelayConfig.defaultRelayURL)
    }

    // MARK: - Private

    private func completeAuth(_ sess: RelaySession) async throws {
        // If a later step throws, this session token is left stored but harmless:
        // the next login overwrites it, and bootstrap ignores a session token that
        // has no matching device token.
        try secrets.save(sess.token, for: Self.kSessionToken)
        username = sess.username
        defaults.set(sess.username, forKey: Self.kUsername)
        if secrets.load(Self.kDeviceToken) == nil {
            let dev = try await client.registerDevice(session: sess.token, label: hostname, role: "either")
            try secrets.save(dev.token, for: Self.kDeviceToken)
            try secrets.save(dev.id, for: Self.kDeviceID)
            localDeviceID = dev.id
        } else {
            localDeviceID = secrets.load(Self.kDeviceID) ?? ""
        }
        let list = try await client.listDevices(session: sess.token)
        peers = list.filter { $0.id != localDeviceID }
        cachePeers(peers)
        state = .loggedIn
        notify()
    }

    private func fail(_ error: Error) {
        lastError = error.localizedDescription
        state = (secrets.load(Self.kDeviceToken) != nil && !localDeviceID.isEmpty) ? .sessionExpired : .loggedOut
        notify()
    }

    private func notify() { onChange?() }

    private func cachePeers(_ peers: [RelayPeerDevice]) {
        if let data = try? JSONEncoder().encode(peers) {
            defaults.set(data, forKey: Self.kDeviceList)
        }
    }
    private func loadCachedPeers() -> [RelayPeerDevice] {
        guard let data = defaults.data(forKey: Self.kDeviceList),
              let peers = try? JSONDecoder().decode([RelayPeerDevice].self, from: data)
        else { return [] }
        return peers
    }
}
