import Foundation

enum OutputOverride: Equatable, Sendable {
    case auto
    case local
    case remote(_ targetId: String)
}

/// Resolves the OutputSink for a recording session, given the frontmost app
/// and any user override.
///
/// Priority:
///   1. Manual override (.local always local, .remote(id) → that target if
///      enabled; if id missing/disabled, falls back to local — never silently
///      selects a different remote)
///   2. Auto: first enabled target whose matchBundleIds contains the frontmost
///      bundle id wins; tie-break is target list order
///   3. Otherwise: local sink
final class OutputRouter: @unchecked Sendable {
    private let localSink: OutputSink
    private let targetsProvider: () -> [OutputTarget]

    init(localSink: OutputSink, targetsProvider: @escaping () -> [OutputTarget]) {
        self.localSink = localSink
        self.targetsProvider = targetsProvider
    }

    func resolve(frontmostBundleId: String?, override: OutputOverride) -> OutputSink {
        let targets = targetsProvider()

        switch override {
        case .local:
            return localSink
        case .remote(let id):
            if let t = targets.first(where: { $0.id == id && $0.enabled }) {
                return RemoteHTTPSink(target: t, fallback: localSink)
            }
            return localSink
        case .auto:
            guard let bundleId = frontmostBundleId else { return localSink }
            for t in targets where t.enabled {
                if t.matchBundleIds.contains(bundleId) {
                    return RemoteHTTPSink(target: t, fallback: localSink)
                }
            }
            return localSink
        }
    }
}
