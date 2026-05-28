import XCTest
import AppKit
@testable import Type4Me

/// Verifies RemoteHTTPSink's clipboard-fallback contract independently of any
/// network transport. Uses a mock RemoteTransport whose result is configured
/// per-test, so we don't depend on TestHTTPServer wire timing.
final class RemoteHTTPSinkClipboardFallbackTests: XCTestCase {
    private var savedClipboard: String?

    override func setUp() {
        super.setUp()
        savedClipboard = NSPasteboard.general.string(forType: .string)
    }

    override func tearDown() {
        NSPasteboard.general.clearContents()
        if let saved = savedClipboard {
            NSPasteboard.general.setString(saved, forType: .string)
        }
        super.tearDown()
    }

    private func makeTarget() -> OutputTarget {
        OutputTarget(id: "test", name: "Test",
                     host: "127.0.0.1", port: 1, token: "t",
                     matchBundleIds: [], enabled: true)
    }

    func testInjectInsertedOnTransportSuccess() {
        let sink = RemoteHTTPSink(target: makeTarget(), transport: MockTransport(result: true))
        XCTAssertEqual(sink.inject("ok"), .inserted)
    }

    func testInjectCopiesToClipboardOnTransportFailure() {
        let sink = RemoteHTTPSink(target: makeTarget(), transport: MockTransport(result: false))
        XCTAssertEqual(sink.inject("fail-content"), .copiedToClipboard)
        XCTAssertEqual(NSPasteboard.general.string(forType: .string), "fail-content")
    }

    func testInjectPassesPreserveClipboardTrue() {
        let mock = MockTransport(result: true)
        let sink = RemoteHTTPSink(target: makeTarget(), transport: mock)
        _ = sink.inject("x")
        XCTAssertEqual(mock.lastPreserveClipboard, true)
    }

    func testInjectGeneratesRequestID() {
        let mock = MockTransport(result: true)
        let sink = RemoteHTTPSink(target: makeTarget(), transport: mock)
        _ = sink.inject("x")
        XCTAssertFalse(mock.lastRequestID.isEmpty)
    }
}

private final class MockTransport: RemoteTransport, @unchecked Sendable {
    let result: Bool
    var lastRequestID: String = ""
    var lastPreserveClipboard: Bool = false

    init(result: Bool) { self.result = result }

    func dispatch(text: String, requestID: String, preserveClipboard: Bool) -> Bool {
        lastRequestID = requestID
        lastPreserveClipboard = preserveClipboard
        return result
    }
}
