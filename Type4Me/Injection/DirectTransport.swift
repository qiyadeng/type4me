import Foundation

/// Sends text via direct HTTP POST to a receiver's listener (LAN mode).
final class DirectTransport: RemoteTransport, @unchecked Sendable {
    private let target: OutputTarget
    private let timeout: TimeInterval
    private let session: URLSession

    init(target: OutputTarget, timeout: TimeInterval = 0.8,
         session: URLSession = .shared) {
        precondition(target.mode == .direct, "DirectTransport requires .direct target")
        self.target = target
        self.timeout = timeout
        self.session = session
    }

    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool {
        let url = target.baseURL.appendingPathComponent("inject")
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(target.token ?? "")", forHTTPHeaderField: "Authorization")
        let body: [String: Any] = [
            "text": text,
            "request_id": requestID,
            "preserve_clipboard": preserveClipboard
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        let resultBox = DirectResultBox()
        let sem = DispatchSemaphore(value: 0)
        let task = session.dataTask(with: req) { data, resp, err in
            resultBox.set((data, resp, err))
            sem.signal()
        }
        task.resume()
        _ = sem.wait(timeout: .now() + timeout + 0.2)
        let result = resultBox.get()
        if result.1 == nil && result.2 == nil {
            task.cancel()
        }
        let finalResult = resultBox.get()
        if finalResult.2 != nil { return false }
        guard let http = finalResult.1 as? HTTPURLResponse, http.statusCode == 200 else {
            return false
        }
        guard let data = finalResult.0,
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              (obj["ok"] as? Bool) == true else {
            return false
        }
        return true
    }
}

/// Lock-protected result box for the URLSession callback ↔ caller race.
private final class DirectResultBox: @unchecked Sendable {
    private let lock = NSLock()
    private var value: (Data?, URLResponse?, Error?) = (nil, nil, nil)
    func set(_ v: (Data?, URLResponse?, Error?)) {
        lock.lock(); value = v; lock.unlock()
    }
    func get() -> (Data?, URLResponse?, Error?) {
        lock.lock(); defer { lock.unlock() }
        return value
    }
}
