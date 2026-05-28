import Foundation

/// A configured remote receiver Type4Me can route ASR text to.
///
/// Persistence: serialized into `credentials.json` under `tf_remote_targets`.
/// NOTE (S2): tokens will move to Keychain when the Settings UI lands.
struct OutputTarget: Codable, Equatable, Identifiable, Sendable {
    enum Mode: String, Codable, Sendable {
        case direct
        case relay
    }

    let id: String
    var name: String
    var enabled: Bool
    var matchBundleIds: [String]

    /// Defaults to `.direct` when missing from JSON (old format compatible).
    var mode: Mode = .direct

    // mode == .direct
    var host: String?
    var port: Int?
    var token: String?

    // mode == .relay
    var relayURL: URL?
    var deviceID: String?
    var deviceToken: String?
    var targetDeviceID: String?

    /// Init keeps the historical labeled-arg order (id, name, host, port, token,
    /// matchBundleIds, enabled) so existing call sites compile unchanged. New
    /// relay-mode fields default to nil.
    init(id: String,
         name: String,
         host: String? = nil,
         port: Int? = nil,
         token: String? = nil,
         matchBundleIds: [String],
         enabled: Bool,
         mode: Mode = .direct,
         relayURL: URL? = nil,
         deviceID: String? = nil,
         deviceToken: String? = nil,
         targetDeviceID: String? = nil) {
        self.id = id
        self.name = name
        self.enabled = enabled
        self.matchBundleIds = matchBundleIds
        self.mode = mode
        self.host = host
        self.port = port
        self.token = token
        self.relayURL = relayURL
        self.deviceID = deviceID
        self.deviceToken = deviceToken
        self.targetDeviceID = targetDeviceID
    }

    enum CodingKeys: String, CodingKey {
        case id, name, enabled, matchBundleIds, mode
        case host, port, token
        case relayURL = "relay_url"
        case deviceID = "device_id"
        case deviceToken = "device_token"
        case targetDeviceID = "target_device_id"
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        self.id = try c.decode(String.self, forKey: .id)
        self.name = try c.decode(String.self, forKey: .name)
        self.enabled = try c.decode(Bool.self, forKey: .enabled)
        self.matchBundleIds = try c.decode([String].self, forKey: .matchBundleIds)
        self.mode = try c.decodeIfPresent(Mode.self, forKey: .mode) ?? .direct
        self.host = try c.decodeIfPresent(String.self, forKey: .host)
        self.port = try c.decodeIfPresent(Int.self, forKey: .port)
        self.token = try c.decodeIfPresent(String.self, forKey: .token)
        self.relayURL = try c.decodeIfPresent(URL.self, forKey: .relayURL)
        self.deviceID = try c.decodeIfPresent(String.self, forKey: .deviceID)
        self.deviceToken = try c.decodeIfPresent(String.self, forKey: .deviceToken)
        self.targetDeviceID = try c.decodeIfPresent(String.self, forKey: .targetDeviceID)
    }

    /// True if the target has all the required fields for its declared mode.
    var isValid: Bool {
        switch mode {
        case .direct:
            return host != nil && port != nil && token != nil
        case .relay:
            return relayURL != nil && deviceID != nil &&
                   deviceToken != nil && targetDeviceID != nil
        }
    }

    /// baseURL for direct mode only. Crashes for relay mode (caller must check).
    var baseURL: URL {
        precondition(mode == .direct, "baseURL is direct-mode only")
        let h = (host!.contains(":") && !host!.contains("[")) ? "[\(host!)]" : host!
        return URL(string: "http://\(h):\(port!)")!
    }
}
