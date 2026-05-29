import XCTest
@testable import Type4Me

final class RemoteTargetResolverTests: XCTestCase {
    private func manual() -> [OutputTarget] {
        [OutputTarget(id: "manual-win", name: "Win", host: "1.1.1.1", port: 1, token: "t",
                      matchBundleIds: ["x"], enabled: true)]
    }
    private func synth() -> [OutputTarget] {
        [OutputTarget(id: "acct-win", name: "AcctWin", matchBundleIds: [], enabled: true,
                      mode: .relay, relayURL: URL(string: "https://r")!, deviceID: "d",
                      deviceToken: "t", targetDeviceID: "acct-win")]
    }

    func testLoggedInUsesSynthesized() {
        let t = RemoteTargetResolver.targets(accountState: .loggedIn, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["acct-win"])
    }
    func testSessionExpiredUsesSynthesized() {
        let t = RemoteTargetResolver.targets(accountState: .sessionExpired, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["acct-win"])
    }
    func testLoggedOutUsesManual() {
        let t = RemoteTargetResolver.targets(accountState: .loggedOut, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["manual-win"])
    }
    func testLoggingInUsesManual() {
        let t = RemoteTargetResolver.targets(accountState: .loggingIn, synthesized: synth, manual: manual)
        XCTAssertEqual(t.map(\.id), ["manual-win"])
    }

    func testSanitizeResetsStaleRemoteOverride() {
        let o = RemoteTargetResolver.sanitizedOverride(.remote("gone"), targets: manual())
        XCTAssertEqual(o, .auto)
    }
    func testSanitizeKeepsValidRemoteOverride() {
        let o = RemoteTargetResolver.sanitizedOverride(.remote("manual-win"), targets: manual())
        XCTAssertEqual(o, .remote("manual-win"))
    }
    func testSanitizeLeavesAutoAndLocal() {
        XCTAssertEqual(RemoteTargetResolver.sanitizedOverride(.auto, targets: []), .auto)
        XCTAssertEqual(RemoteTargetResolver.sanitizedOverride(.local, targets: []), .local)
    }
}
