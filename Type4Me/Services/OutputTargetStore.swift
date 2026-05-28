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
    /// Filters out entries whose `mode`-specific required fields are missing.
    func load() -> [OutputTarget] {
        guard let data = try? Data(contentsOf: credentialsFile),
              let dict = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let arr = dict[Self.jsonKey] as? [[String: Any]]
        else { return [] }

        var out: [OutputTarget] = []
        for entry in arr {
            guard let entryData = try? JSONSerialization.data(withJSONObject: entry),
                  let target = try? JSONDecoder().decode(OutputTarget.self, from: entryData),
                  target.isValid
            else { continue }
            out.append(target)
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
