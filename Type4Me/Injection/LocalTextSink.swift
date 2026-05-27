import Foundation

/// OutputSink that pastes into the frontmost macOS app, using the existing
/// TextInjectionEngine. This is a thin wrapper to allow the engine to be
/// swapped for remote sinks at routing time.
final class LocalTextSink: OutputSink {
    private let engine: TextInjectionEngine

    init(engine: TextInjectionEngine = TextInjectionEngine()) {
        self.engine = engine
    }

    func inject(_ text: String) -> InjectionOutcome {
        engine.inject(text)
    }

    /// Pass-through: expose preserveClipboard so RecognitionSession can still
    /// configure it from defaults.
    var preserveClipboard: Bool {
        get { engine.preserveClipboard }
        set { engine.preserveClipboard = newValue }
    }

    /// Pass-through for the "finalize restore" lifecycle the engine already has.
    func finishClipboardRestore() {
        engine.finishClipboardRestore()
    }

    func copyToClipboard(_ text: String, transient: Bool = false) {
        engine.copyToClipboard(text, transient: transient)
    }
}
