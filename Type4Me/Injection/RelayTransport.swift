import Foundation

/// Sends text via HTTP POST to a relay's /v1/dispatch endpoint.
final class RelayTransport: RemoteTransport, @unchecked Sendable {
    private let target: OutputTarget
    private let timeout: TimeInterval
    private let session: URLSession

    init(target: OutputTarget, timeout: TimeInterval = 0.8,
         session: URLSession = .shared) {
        precondition(target.mode == .relay, "RelayTransport requires .relay target")
        self.target = target
        self.timeout = timeout
        self.session = session
    }

    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool {
        let url = target.relayURL!.appendingPathComponent("v1/dispatch")
        var req = URLRequest(url: url, timeoutInterval: timeout)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.setValue("Bearer \(target.deviceToken!)", forHTTPHeaderField: "Authorization")
        let body: [String: Any] = [
            "target_device_id": target.targetDeviceID!,
            "text": text,
            "request_id": requestID,
            "preserve_clipboard": preserveClipboard
        ]
        req.httpBody = try? JSONSerialization.data(withJSONObject: body)

        let resultBox = RelayResultBox()
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
        guard let http = finalResult.1 as? HTTPURLResponse else { return false }
        // relay returns 202 on accepted; anything else (401/403/404/413/503) is failure
        return http.statusCode == 202
    }
}

private final class RelayResultBox: @unchecked Sendable {
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
