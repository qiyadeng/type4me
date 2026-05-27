import Foundation

/// A configured remote receiver Type4Me can route ASR text to.
///
/// Persistence: serialized into `credentials.json` under `tf_remote_targets`.
/// NOTE (S2): `token` will move to Keychain when the Settings UI lands.
struct OutputTarget: Codable, Equatable, Identifiable, Sendable {
    /// Stable id, e.g. "win-pc" or a UUID. Used in HistoryStore.target_id and Keychain account.
    let id: String
    /// Human-readable name displayed in the menu bar override.
    var name: String
    /// Hostname or IP. IPv6 addresses are bracketed automatically in baseURL.
    var host: String
    /// TCP port the receiver listens on. Default 47318.
    var port: Int
    /// Bearer token for `Authorization: Bearer <token>`.
    var token: String
    /// macOS bundle ids that, when frontmost, route to this target.
    var matchBundleIds: [String]
    /// If false, this target is skipped during auto-route.
    var enabled: Bool

    var baseURL: URL {
        // Bracket IPv6 literals: detect by presence of ':' without prior bracket.
        let h = (host.contains(":") && !host.contains("[")) ? "[\(host)]" : host
        return URL(string: "http://\(h):\(port)")!
    }
}
