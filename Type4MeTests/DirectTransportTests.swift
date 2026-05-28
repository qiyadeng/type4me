import XCTest
@testable import Type4Me

final class DirectTransportTests: XCTestCase {
    private var server: TestHTTPServer!
    private var target: OutputTarget!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        target = OutputTarget(
            id: "test", name: "Test", host: "127.0.0.1", port: server.port,
            token: "tok-123", matchBundleIds: [], enabled: true
        )
    }

    override func tearDown() {
        server.stop()
        server = nil
        super.tearDown()
    }

    func testDispatchSendsBearerTokenAndJSONBody() {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/inject")
            XCTAssertEqual(req.headers["Authorization"], "Bearer tok-123")
            XCTAssertEqual(req.headers["Content-Type"], "application/json")
            guard let bodyData = req.body.data(using: .utf8),
                  let parsed = try? JSONSerialization.jsonObject(with: bodyData) as? [String: Any]
            else {
                XCTFail("body not valid JSON: \(req.body)")
                return TestHTTPResponse(status: 500, body: "")
            }
            XCTAssertEqual(parsed["text"] as? String, "你好")
            XCTAssertEqual(parsed["request_id"] as? String, "req-1")
            XCTAssertEqual(parsed["preserve_clipboard"] as? Bool, true)
            return TestHTTPResponse(status: 200, body: #"{"ok":true}"#)
        }
        let t = DirectTransport(target: target)
        XCTAssertTrue(t.dispatch(text: "你好", requestID: "req-1", preserveClipboard: true))
    }

    func testDispatch401ReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"x"}"#) }
        let t = DirectTransport(target: target)
        XCTAssertFalse(t.dispatch(text: "x", requestID: "r", preserveClipboard: false))
    }

    func testDispatchOkFalseReturnsFalse() {
        server.respond { _ in
            TestHTTPResponse(status: 200, body: #"{"ok":false,"reason":"no-focus"}"#)
        }
        let t = DirectTransport(target: target)
        XCTAssertFalse(t.dispatch(text: "x", requestID: "r", preserveClipboard: false))
    }

    func testDispatchTimeoutReturnsFalse() {
        server.respond { _ in
            Thread.sleep(forTimeInterval: 2.0)
            return TestHTTPResponse(status: 200, body: "{}")
        }
        let t = DirectTransport(target: target, timeout: 0.3)
        XCTAssertFalse(t.dispatch(text: "x", requestID: "r", preserveClipboard: false))
    }

    func testDispatchConnectionRefusedReturnsFalse() {
        let dead = OutputTarget(id: "x", name: "x", host: "127.0.0.1", port: 1,
                                token: "t", matchBundleIds: [], enabled: true)
        let t = DirectTransport(target: dead, timeout: 0.3)
        XCTAssertFalse(t.dispatch(text: "x", requestID: "r", preserveClipboard: false))
    }
}
