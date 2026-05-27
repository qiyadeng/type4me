import XCTest
import AppKit
@testable import Type4Me

final class RemoteHTTPSinkTests: XCTestCase {
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

    func testInjectSendsBearerTokenAndJSONBody() {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/inject")
            XCTAssertEqual(req.headers["Authorization"], "Bearer tok-123")
            XCTAssertEqual(req.headers["Content-Type"], "application/json")
            XCTAssertTrue(req.body.contains("\"text\":\"你好\""))
            return TestHTTPResponse(status: 200,
                body: #"{"ok":true,"outcome":{"pasted":true}}"#)
        }
        let sink = RemoteHTTPSink(target: target)
        let outcome = sink.inject("你好")
        XCTAssertEqual(outcome, .inserted)
    }

    func testInject401ReturnsCopiedToClipboard() {
        let pb = NSPasteboard.general
        let saved = pb.string(forType: .string)
        defer {
            pb.clearContents()
            if let saved { pb.setString(saved, forType: .string) }
        }
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_token"}"#) }
        let sink = RemoteHTTPSink(target: target)
        let outcome = sink.inject("hi-401")
        XCTAssertEqual(outcome, .copiedToClipboard)
        XCTAssertEqual(pb.string(forType: .string), "hi-401")
    }

    func testInjectTimeoutCopiesToClipboard() {
        let pb = NSPasteboard.general
        let saved = pb.string(forType: .string)
        defer {
            pb.clearContents()
            if let saved { pb.setString(saved, forType: .string) }
        }
        server.respond { _ in
            Thread.sleep(forTimeInterval: 2.0)
            return TestHTTPResponse(status: 200, body: "{}")
        }
        let sink = RemoteHTTPSink(target: target, timeout: 0.3)
        let outcome = sink.inject("hi-timeout")
        XCTAssertEqual(outcome, .copiedToClipboard)
        XCTAssertEqual(pb.string(forType: .string), "hi-timeout")
    }

    func testInjectConnectionRefused() {
        let pb = NSPasteboard.general
        let saved = pb.string(forType: .string)
        defer {
            pb.clearContents()
            if let saved { pb.setString(saved, forType: .string) }
        }
        let dead = OutputTarget(id: "x", name: "x", host: "127.0.0.1", port: 1,
                                token: "t", matchBundleIds: [], enabled: true)
        let sink = RemoteHTTPSink(target: dead, timeout: 0.3)
        let outcome = sink.inject("hi-refused")
        XCTAssertEqual(outcome, .copiedToClipboard)
        XCTAssertEqual(pb.string(forType: .string), "hi-refused")
    }

    func testInjectOkFalseWithReason() {
        server.respond { _ in
            TestHTTPResponse(status: 200,
                body: #"{"ok":false,"outcome":{"pasted":false,"reason":"no-focus"}}"#)
        }
        let sink = RemoteHTTPSink(target: target)
        let outcome = sink.inject("hi-noaccept")
        // Server didn't accept the paste; we treat that as a failure and copy to clipboard.
        XCTAssertEqual(outcome, .copiedToClipboard)
    }
}
