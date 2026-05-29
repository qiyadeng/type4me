import Foundation

/// Decides which target source feeds the router/selector, and sanitizes a stale
/// override after the source changes. Pure — no AppState dependency.
enum RemoteTargetResolver {
    static func targets(accountState: AccountSession.State,
                        synthesized: () -> [OutputTarget],
                        manual: () -> [OutputTarget]) -> [OutputTarget] {
        switch accountState {
        case .loggedIn, .sessionExpired: return synthesized()
        case .loggedOut, .loggingIn: return manual()
        }
    }

    /// If `override` points at a `.remote(id)` not present in `targets`, reset to `.auto`.
    static func sanitizedOverride(_ override: OutputOverride, targets: [OutputTarget]) -> OutputOverride {
        if case let .remote(id) = override, !targets.contains(where: { $0.id == id }) {
            return .auto
        }
        return override
    }
}
