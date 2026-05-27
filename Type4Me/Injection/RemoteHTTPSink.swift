import Foundation
import AppKit

/// OutputSink that posts text to a remote receiver over HTTP.
///
/// Synchronous: blocks the calling thread for up to `timeout` seconds.
/// RecognitionSession already calls this from a Task.detached, so the block is safe.
///
/// Failure handling: ANY non-success outcome (timeout, 401, 413, 423, 5xx,
/// or HTTP 200 with `ok:false`) falls back to writing the text to the
/// system pasteboard so the user can paste it manually. This is unconditional
/// because the InjectionOutcome type has no `.failed` case — losing the
/// transcript silently would be the worst outcome.
final class RemoteHTTPSink: OutputSink, @unchecked Sendable {
    let target: OutputTarget
    private let timeout: TimeInterval
    private let session: URLSession

    init(target: OutputTarget,
         timeout: TimeInterval = 0.8,
         session: URLSession = .shared) {
        self.target = target
        self.timeout = timeout
        self.session = session
    }

    func inject(_ text: String) -> InjectionOutcome {
        let url = target.baseURL.appendingPathComponent("inject")
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(target.token)", forHTTPHeaderField: "Authorization")

        let body: [String: Any] = [
            "text": text,
            "request_id": UUID().uuidString,
            "preserve_clipboard": true
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        // Synchronous bridge over URLSession's async API.
        let sem = DispatchSemaphore(value: 0)
        var result: (Data?, URLResponse?, Error?) = (nil, nil, nil)
        let task = session.dataTask(with: req) { data, resp, err in
            result = (data, resp, err)
            sem.signal()
        }
        task.resume()
        _ = sem.wait(timeout: .now() + timeout + 0.2)
        // If the semaphore timed out before URLSession finished, cancel the task so its
        // callback won't continue mutating `result` after we return.
        if result.1 == nil && result.2 == nil {
            task.cancel()
        }

        if result.2 != nil {
            return copyToClipboardFallback(text)
        }
        guard let http = result.1 as? HTTPURLResponse else {
            return copyToClipboardFallback(text)
        }
        if http.statusCode == 200,
           let data = result.0,
           let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
           (obj["ok"] as? Bool) == true {
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
