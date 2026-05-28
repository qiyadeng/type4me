import XCTest
@testable import Type4Me

final class RelayTransportTests: XCTestCase {
    private var server: TestHTTPServer!
    private var target: OutputTarget!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        target = OutputTarget(
            id: "rly", name: "Rly",
            matchBundleIds: [],
            enabled: true,
            mode: .relay,
            relayURL: URL(string: "http://127.0.0.1:\(server.port)")!,
            deviceID: "dev-Mac",
            deviceToken: "tok-mac",
            targetDeviceID: "dev-Win"
        )
    }
    override func tearDown() { server.stop(); server = nil; super.tearDown() }

    func testDispatchPathAndBody() {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/v1/dispatch")
            XCTAssertEqual(req.headers["Authorization"], "Bearer tok-mac")
            guard let body = req.body.data(using: .utf8),
                  let parsed = try? JSONSerialization.jsonObject(with: body) as? [String: Any] else {
                XCTFail("bad body: \(req.body)")
                return TestHTTPResponse(status: 500, body: "")
            }
            XCTAssertEqual(parsed["target_device_id"] as? String, "dev-Win")
            XCTAssertEqual(parsed["text"] as? String, "你好")
            return TestHTTPResponse(status: 202, body: #"{"accepted":true,"message_id":"msg-1"}"#)
        }
        let t = RelayTransport(target: target)
        XCTAssertTrue(t.dispatch(text: "你好", requestID: "r", preserveClipboard: true))
    }

    func testDispatch401ReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_sender_token"}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatch403CrossAccountReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 403, body: #"{"error":"cross_account"}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatch503ReceiverOfflineReturnsFalse() {
        server.respond { _ in TestHTTPResponse(status: 503, body: #"{"error":"receiver_offline"}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatchTimeoutReturnsFalse() {
        server.respond { _ in
            Thread.sleep(forTimeInterval: 2.0)
            return TestHTTPResponse(status: 202, body: "{}")
        }
        let t = RelayTransport(target: target, timeout: 0.3)
        XCTAssertFalse(t.dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }

    func testDispatch200NotEnoughIsFailure() {
        server.respond { _ in TestHTTPResponse(status: 200, body: #"{"accepted":true}"#) }
        XCTAssertFalse(RelayTransport(target: target).dispatch(text: "x", requestID: "r", preserveClipboard: true))
    }
}
