import XCTest
@testable import Type4Me

final class InMemorySecretStore: SecretStore, @unchecked Sendable {
    private var store: [String: String] = [:]
    func load(_ key: String) -> String? { store[key] }
    func save(_ value: String, for key: String) throws { store[key] = value }
    @discardableResult func delete(_ key: String) -> Bool { store.removeValue(forKey: key) != nil }
}

final class FakeRelayClient: RelayAccountClienting, @unchecked Sendable {
    var session = RelaySession(token: "sess", accountID: "acct", username: "alice", expiresAt: nil)
    var device = RelayRegisteredDevice(id: "dev-mac", token: "mac-tok", label: "Mac", role: "either")
    var devices: [RelayPeerDevice] = []
    var loginError: Error?
    var registerError: Error?
    var registerDeviceError: Error?
    var listError: Error?
    private(set) var loginCount = 0, registerCount = 0, registerDeviceCount = 0, listCount = 0
    var lastInvite = ""

    func login(username: String, password: String) async throws -> RelaySession {
        loginCount += 1; if let e = loginError { throw e }; return session
    }
    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession {
        registerCount += 1; lastInvite = inviteCode; if let e = registerError { throw e }; return session
    }
    func registerDevice(session: String, label: String, role: String) async throws -> RelayRegisteredDevice {
        registerDeviceCount += 1; if let e = registerDeviceError { throw e }; return device
    }
    func listDevices(session: String) async throws -> [RelayPeerDevice] {
        listCount += 1; if let e = listError { throw e }; return devices
    }
}

@MainActor
final class AccountSessionTests: XCTestCase {
    private func makeDefaults() -> UserDefaults { UserDefaults(suiteName: "t-\(UUID().uuidString)")! }

    private func makeSession(client: FakeRelayClient = FakeRelayClient(),
                             secrets: InMemorySecretStore = InMemorySecretStore()) -> AccountSession {
        AccountSession(client: client, secrets: secrets, defaults: makeDefaults(), hostname: "TestMac")
    }

    func testLoginRegistersDeviceAndGoesLoggedIn() async {
        let client = FakeRelayClient()
        client.devices = [RelayPeerDevice(id: "dev-win", label: "Win", online: true),
                          RelayPeerDevice(id: "dev-mac", label: "Mac", online: true)]
        let secrets = InMemorySecretStore()
        let s = makeSession(client: client, secrets: secrets)
        await s.login(username: "alice", password: "supersecret")
        XCTAssertEqual(s.state, .loggedIn)
        XCTAssertEqual(client.registerDeviceCount, 1)
        XCTAssertEqual(s.localDeviceID, "dev-mac")
        XCTAssertEqual(s.peers.map(\.id), ["dev-win"], "own device filtered out")
        XCTAssertEqual(secrets.load(AccountSession.kDeviceToken), "mac-tok")
        XCTAssertEqual(s.synthesizedTargets().map(\.id), ["dev-win"])
    }

    func testLoginSkipsDeviceRegistrationWhenTokenExists() async {
        let client = FakeRelayClient()
        let secrets = InMemorySecretStore()
        try? secrets.save("existing-tok", for: AccountSession.kDeviceToken)
        try? secrets.save("dev-mac", for: AccountSession.kDeviceID)
        let s = makeSession(client: client, secrets: secrets)
        await s.login(username: "alice", password: "supersecret")
        XCTAssertEqual(client.registerDeviceCount, 0)
        XCTAssertEqual(s.localDeviceID, "dev-mac")
    }

    func testRegisterModeSendsInvite() async {
        let client = FakeRelayClient()
        let s = makeSession(client: client)
        await s.register(username: "bob", password: "supersecret", inviteCode: "INV-1")
        XCTAssertEqual(client.registerCount, 1)
        XCTAssertEqual(client.lastInvite, "INV-1")
        XCTAssertEqual(s.state, .loggedIn)
    }

    func testLoginFailureGoesLoggedOutWithError() async {
        let client = FakeRelayClient()
        client.loginError = RelayAPIError.api(status: 401, code: "invalid_credentials", message: "用户名或密码错误")
        let s = makeSession(client: client)
        await s.login(username: "alice", password: "wrong")
        XCTAssertEqual(s.state, .loggedOut)
        XCTAssertEqual(s.lastError, "用户名或密码错误")
        XCTAssertEqual(client.registerDeviceCount, 0)
    }

    func testRefreshSessionExpiredOn401() async {
        let client = FakeRelayClient()
        let secrets = InMemorySecretStore()
        try? secrets.save("sess", for: AccountSession.kSessionToken)
        try? secrets.save("mac-tok", for: AccountSession.kDeviceToken)
        try? secrets.save("dev-mac", for: AccountSession.kDeviceID)
        client.listError = RelayAPIError.api(status: 401, code: "invalid_session", message: "登录已过期,请重新登录")
        let s = makeSession(client: client, secrets: secrets)
        s.bootstrap()
        XCTAssertEqual(s.state, .loggedIn)
        await s.refreshDevices()
        XCTAssertEqual(s.state, .sessionExpired)
    }

    func testBootstrapLoggedOutWhenNoDeviceToken() {
        let s = makeSession()
        s.bootstrap()
        XCTAssertEqual(s.state, .loggedOut)
    }

    func testLogoutClearsEverything() async {
        let client = FakeRelayClient()
        client.devices = [RelayPeerDevice(id: "dev-win", label: "Win", online: true)]
        let secrets = InMemorySecretStore()
        let s = makeSession(client: client, secrets: secrets)
        await s.login(username: "alice", password: "supersecret")
        XCTAssertEqual(s.state, .loggedIn)
        s.logout()
        XCTAssertEqual(s.state, .loggedOut)
        XCTAssertNil(secrets.load(AccountSession.kSessionToken))
        XCTAssertNil(secrets.load(AccountSession.kDeviceToken))
        XCTAssertNil(secrets.load(AccountSession.kDeviceID))
        XCTAssertTrue(s.peers.isEmpty)
        XCTAssertTrue(s.synthesizedTargets().isEmpty)
    }

    func testOnChangeFiresOnStateTransitions() async {
        let s = makeSession()
        var count = 0
        s.onChange = { count += 1 }
        await s.login(username: "alice", password: "supersecret")
        XCTAssertGreaterThan(count, 0)
        let before = count
        s.logout()
        XCTAssertGreaterThan(count, before)
    }

    func testRefreshClearsStaleError() async {
        let client = FakeRelayClient()
        let secrets = InMemorySecretStore()
        try? secrets.save("sess", for: AccountSession.kSessionToken)
        try? secrets.save("mac-tok", for: AccountSession.kDeviceToken)
        try? secrets.save("dev-mac", for: AccountSession.kDeviceID)
        client.devices = [RelayPeerDevice(id: "dev-win", label: "Win", online: true)]
        let s = makeSession(client: client, secrets: secrets)
        s.bootstrap()
        s.lastError = "stale"
        await s.refreshDevices()
        XCTAssertEqual(s.state, .loggedIn)
        XCTAssertNil(s.lastError)
    }
}
