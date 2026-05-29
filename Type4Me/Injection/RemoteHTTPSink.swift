import Foundation
import AppKit

/// OutputSink that delegates the actual HTTP transport to RemoteTransport
/// (DirectTransport for LAN mode, RelayTransport for relay mode), and
/// handles Outcome mapping + clipboard fallback.
///
/// Failure handling: on transport failure, if a `fallback` sink is provided
/// the text is delegated to it (local injection); otherwise the text is
/// written to the system pasteboard and returns .copiedToClipboard. Either
/// way the transcript is never lost silently — the worst outcome.
final class RemoteHTTPSink: OutputSink, @unchecked Sendable {
    let target: OutputTarget
    private let transport: RemoteTransport
    /// Optional local-injection sink used when the remote transport fails.
    /// When nil, failures fall back to clipboard (legacy behaviour).
    let fallback: OutputSink?

    init(target: OutputTarget, fallback: OutputSink? = nil) {
        self.target = target
        self.fallback = fallback
        switch target.mode {
        case .direct:
            self.transport = DirectTransport(target: target)
        case .relay:
            self.transport = RelayTransport(target: target)
        }
    }

    /// Injection point used by OutputRouter / tests that need a custom transport.
    init(target: OutputTarget, transport: RemoteTransport, fallback: OutputSink? = nil) {
        self.target = target
        self.transport = transport
        self.fallback = fallback
    }

    func inject(_ text: String) -> InjectionOutcome {
        let requestID = UUID().uuidString
        if transport.dispatch(text: text, requestID: requestID, preserveClipboard: true) {
            return .inserted
        }
        if let fallback {
            return fallback.inject(text)
        }
        return copyToClipboardFallback(text)
    }

    private func copyToClipboardFallback(_ text: String) -> InjectionOutcome {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        return .copiedToClipboard
    }
}
