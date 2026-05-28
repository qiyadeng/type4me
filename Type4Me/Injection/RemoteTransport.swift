import Foundation

/// Abstracts the "send text to a remote endpoint" detail away from
/// RemoteHTTPSink. Two implementations:
///
/// - DirectTransport: POSTs to a receiver's HTTP server on a known host:port
/// - RelayTransport: POSTs to a relay server's /v1/dispatch endpoint
///
/// Synchronous: blocks the calling thread for up to ~800 ms.
/// RemoteHTTPSink already calls inject from a Task.detached, so blocking is safe.
protocol RemoteTransport: Sendable {
    /// Send `text` to the configured remote. Return true on success.
    /// Failure (network error, auth error, anything non-success) returns false;
    /// caller (RemoteHTTPSink) writes text to clipboard and reports .copiedToClipboard.
    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool
}
