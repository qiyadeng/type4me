import XCTest
@testable import Type4Me

final class OutputRouterTests: XCTestCase {
    private let local = LocalTextSink()
    private let winTarget = OutputTarget(
        id: "win", name: "Win", host: "1.1.1.1", port: 47318,
        token: "t", matchBundleIds: ["com.microsoft.rdc.macos"], enabled: true
    )
    private let macTarget = OutputTarget(
        id: "mac2", name: "Mac2", host: "1.1.1.2", port: 47318,
        token: "t", matchBundleIds: ["com.apple.ScreenSharingViewer"], enabled: true
    )

    private func makeRouter(targets: [OutputTarget]) -> OutputRouter {
        OutputRouter(localSink: local, targetsProvider: { targets })
    }

    func testManualOverrideLocalAlwaysWins() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .local)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testManualOverrideRemoteSelectsTarget() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: nil,
                                  override: .remote("win"))
        XCTAssertTrue(sink is RemoteHTTPSink)
    }

    func testManualOverrideRemoteUnknownIdFallsBackToLocal() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .remote("missing"))
        XCTAssertTrue(sink is LocalTextSink, "missing target id should not silently select another")
    }

    func testAutoMatchesBundleId() {
        let router = makeRouter(targets: [winTarget, macTarget])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .auto)
        XCTAssertTrue(sink is RemoteHTTPSink)
        XCTAssertEqual((sink as? RemoteHTTPSink)?.target.id, "win")
    }

    func testAutoNoMatchReturnsLocal() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: "com.apple.finder",
                                  override: .auto)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testAutoFrontmostNilReturnsLocal() {
        let router = makeRouter(targets: [winTarget])
        let sink = router.resolve(frontmostBundleId: nil, override: .auto)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testAutoDisabledTargetSkipped() {
        var disabled = winTarget
        disabled.enabled = false
        let router = makeRouter(targets: [disabled])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .auto)
        XCTAssertTrue(sink is LocalTextSink)
    }

    func testAutoFirstMatchWinsOnPriority() {
        let alt2 = OutputTarget(
            id: "win-alt", name: "Win-alt", host: "1.1.1.9", port: 47318,
            token: "t", matchBundleIds: ["com.microsoft.rdc.macos"], enabled: true
        )
        let router = makeRouter(targets: [winTarget, alt2])
        let sink = router.resolve(frontmostBundleId: "com.microsoft.rdc.macos",
                                  override: .auto)
        XCTAssertEqual((sink as? RemoteHTTPSink)?.target.id, "win")
    }
}
