import Foundation

/// Abstraction for "where injected text goes".
///
/// Implementations:
/// - LocalTextSink: pastes into the frontmost macOS app via NSPasteboard + CGEvent
/// - RemoteHTTPSink: POSTs text to a remote receiver over HTTP
protocol OutputSink {
    /// Synchronously inject `text` and return the outcome.
    /// MUST be safe to call from a Task.detached context (see RecognitionSession).
    func inject(_ text: String) -> InjectionOutcome
}
