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

    func testInjectDelegatesToFallbackOnTransportFailure() {
        let fallback = RecordingSink(outcome: .inserted)
        let sink = RemoteHTTPSink(target: makeTarget(),
                                  transport: MockTransport(result: false),
                                  fallback: fallback)
        let outcome = sink.inject("to-fallback")
        XCTAssertEqual(fallback.injected, ["to-fallback"], "失败时应把文本交给本机 fallback")
        XCTAssertEqual(outcome, .inserted, "应返回 fallback 的结果")
    }

    func testInjectDoesNotUseFallbackOnTransportSuccess() {
        let fallback = RecordingSink(outcome: .inserted)
        let sink = RemoteHTTPSink(target: makeTarget(),
                                  transport: MockTransport(result: true),
                                  fallback: fallback)
        XCTAssertEqual(sink.inject("ok"), .inserted)
        XCTAssertTrue(fallback.injected.isEmpty, "成功时不应触发 fallback")
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

private final class RecordingSink: OutputSink, @unchecked Sendable {
    let outcome: InjectionOutcome
    var injected: [String] = []
    init(outcome: InjectionOutcome) { self.outcome = outcome }
    func inject(_ text: String) -> InjectionOutcome {
        injected.append(text)
        return outcome
    }
}
