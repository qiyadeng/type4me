import Foundation

/// Loads OutputTarget entries from `credentials.json` (key `tf_remote_targets`).
/// S1 stores all fields (including token) in the JSON file. S2 will migrate
/// the token to Keychain when the Settings UI is introduced.
final class OutputTargetStore {
    static let jsonKey = "tf_remote_targets"

    private let credentialsFile: URL

    init(credentialsFile: URL = OutputTargetStore.defaultCredentialsFile) {
        self.credentialsFile = credentialsFile
    }

    /// Returns the OutputTarget array, or empty if the file/key is missing or malformed.
    func load() -> [OutputTarget] {
        guard let data = try? Data(contentsOf: credentialsFile),
              let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let arr = dict[Self.jsonKey] as? [[String: Any]]
        else { return [] }

        var out: [OutputTarget] = []
        for entry in arr {
            guard
                let id = entry["id"] as? String,
                let name = entry["name"] as? String,
                let host = entry["host"] as? String,
                let port = entry["port"] as? Int,
                let token = entry["token"] as? String,
                let bundleIds = entry["matchBundleIds"] as? [String],
                let enabled = entry["enabled"] as? Bool
            else { continue }
            out.append(OutputTarget(
                id: id, name: name, host: host, port: port, token: token,
                matchBundleIds: bundleIds, enabled: enabled
            ))
        }
        return out
    }

    static var defaultCredentialsFile: URL {
        let dir = FileManager.default
            .urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("Type4Me")
        return dir.appendingPathComponent("credentials.json")
    }
}
