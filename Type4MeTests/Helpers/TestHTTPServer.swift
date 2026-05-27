import Foundation
import Network

/// Minimal HTTP/1.1 server for in-process tests. Single-request lifecycle.
/// Not RFC-compliant; just enough to assert headers/body and return canned responses.
final class TestHTTPServer: @unchecked Sendable {
    struct Request {
        let method: String
        let path: String
        let headers: [String: String]
        let body: String
    }
    typealias Responder = (Request) -> TestHTTPResponse

    private(set) nonisolated(unsafe) var port: Int = 0
    private nonisolated(unsafe) var listener: NWListener?
    private nonisolated(unsafe) var responder: Responder = { _ in TestHTTPResponse(status: 500, body: "no responder") }

    func start() {
        let listener = try! NWListener(using: .tcp, on: .any)
        self.listener = listener
        listener.newConnectionHandler = { [weak self] conn in
            self?.handle(conn)
        }
        let group = DispatchGroup()
        group.enter()
        listener.stateUpdateHandler = { state in
            if case .ready = state { group.leave() }
        }
        listener.start(queue: .global())
        _ = group.wait(timeout: .now() + 2)
        self.port = Int(listener.port?.rawValue ?? 0)
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    func respond(_ r: @escaping Responder) {
        self.responder = r
    }

    private func handle(_ conn: NWConnection) {
        conn.start(queue: .global())
        readFullRequest(conn, accumulated: Data())
    }

    /// Accumulate reads until we have the full HTTP request (headers + Content-Length body bytes).
    /// XCTest's process structure occasionally splits headers and body into separate TCP reads on
    /// loopback, so a single `receive` is not enough.
    private func readFullRequest(_ conn: NWConnection, accumulated: Data) {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, isComplete, _ in
            guard let self else { conn.cancel(); return }
            let buf = accumulated + (data ?? Data())
            // Find end of headers
            guard let raw = String(data: buf, encoding: .utf8) else {
                conn.cancel(); return
            }
            guard let sepRange = raw.range(of: "\r\n\r\n") else {
                if isComplete { self.respond(conn, raw: raw); return }
                self.readFullRequest(conn, accumulated: buf)
                return
            }
            let head = String(raw[..<sepRange.lowerBound])
            let bodyStr = String(raw[sepRange.upperBound...])
            let contentLength = self.contentLength(from: head)
            if bodyStr.utf8.count >= contentLength || isComplete {
                self.respond(conn, raw: raw)
            } else {
                self.readFullRequest(conn, accumulated: buf)
            }
        }
    }

    private func contentLength(from head: String) -> Int {
        for line in head.components(separatedBy: "\r\n") {
            if line.lowercased().hasPrefix("content-length:") {
                if let idx = line.firstIndex(of: ":") {
                    return Int(line[line.index(after: idx)...].trimmingCharacters(in: .whitespaces)) ?? 0
                }
            }
        }
        return 0
    }

    private func respond(_ conn: NWConnection, raw: String) {
        let req = self.parse(raw)
        let resp = self.responder(req)
        let respData = self.serialize(resp)
        conn.send(content: respData, completion: .contentProcessed { _ in conn.cancel() })
    }

    private func parse(_ raw: String) -> Request {
        let parts = raw.components(separatedBy: "\r\n\r\n")
        let head = parts.first ?? ""
        let body = parts.count > 1 ? parts[1] : ""
        var lines = head.components(separatedBy: "\r\n")
        let reqLine = lines.removeFirst().components(separatedBy: " ")
        var headers: [String: String] = [:]
        for line in lines {
            if let idx = line.firstIndex(of: ":") {
                let k = String(line[..<idx])
                let v = String(line[line.index(after: idx)...]).trimmingCharacters(in: .whitespaces)
                headers[k] = v
            }
        }
        return Request(
            method: reqLine.count > 0 ? reqLine[0] : "",
            path: reqLine.count > 1 ? reqLine[1] : "/",
            headers: headers,
            body: body
        )
    }

    private func serialize(_ r: TestHTTPResponse) -> Data {
        let bodyData = r.body.data(using: .utf8) ?? Data()
        var head = "HTTP/1.1 \(r.status) OK\r\n"
        head += "Content-Type: application/json\r\n"
        head += "Content-Length: \(bodyData.count)\r\n"
        head += "Connection: close\r\n\r\n"
        return (head.data(using: .utf8) ?? Data()) + bodyData
    }
}

struct TestHTTPResponse {
    let status: Int
    let body: String
}
