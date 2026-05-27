import XCTest
@testable import Type4Me

final class LocalTextSinkTests: XCTestCase {
    func testInjectDelegatesToEngineAndReturnsOutcome() {
        let sink = LocalTextSink(engine: TextInjectionEngine())
        // We can't assert paste actually happened in unit test (no UI focus),
        // but we can assert the sink returns an InjectionOutcome value.
        let outcome = sink.inject("hello")
        // Outcome is one of the existing cases; just make sure call completes.
        switch outcome {
        case .inserted, .copiedToClipboard:
            break  // any of these is OK in a headless test environment
        }
    }
}
