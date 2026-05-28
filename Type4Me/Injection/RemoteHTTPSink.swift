import Foundation
import AppKit

/// OutputSink that delegates the actual HTTP transport to RemoteTransport
/// (DirectTransport for LAN mode, RelayTransport for relay mode), and
/// handles Outcome mapping + clipboard fallback.
///
/// Failure handling: ANY transport failure unconditionally writes the text
/// to the system pasteboard and returns .copiedToClipboard. This is the
/// same guarantee as pre-refactor — losing the transcript silently is the
/// worst outcome.
final class RemoteHTTPSink: OutputSink, @unchecked Sendable {
    let target: OutputTarget
    private let transport: RemoteTransport

    init(target: OutputTarget) {
        self.target = target
        switch target.mode {
        case .direct:
            self.transport = DirectTransport(target: target)
        case .relay:
            self.transport = RelayTransport(target: target)
        }
    }

    /// Injection point used by OutputRouter / tests that need a custom transport.
    init(target: OutputTarget, transport: RemoteTransport) {
        self.target = target
        self.transport = transport
    }

    func inject(_ text: String) -> InjectionOutcome {
        let requestID = UUID().uuidString
        if transport.dispatch(text: text, requestID: requestID, preserveClipboard: true) {
            return .inserted
        }
        return copyToClipboardFallback(text)
    }

    private func copyToClipboardFallback(_ text: String) -> InjectionOutcome {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        return .copiedToClipboard
    }
}
