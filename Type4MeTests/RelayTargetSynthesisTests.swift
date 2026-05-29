import XCTest
@testable import Type4Me

final class RelayTargetSynthesisTests: XCTestCase {
    private let relayURL = URL(string: "https://relay.example.com")!

    func testMapsPeersExcludingSelf() {
        let peers = [
            RelayPeerDevice(id: "dev-win", label: "Win", online: true),
            RelayPeerDevice(id: "dev-mac", label: "ThisMac", online: true),
        ]
        let targets = RelayTargetSynthesis.targets(
            peers: peers, localDeviceID: "dev-mac", deviceToken: "mac-tok", relayURL: relayURL)
        XCTAssertEqual(targets.count, 1, "own device must be excluded")
        let t = targets[0]
        XCTAssertEqual(t.id, "dev-win")
        XCTAssertEqual(t.name, "Win")
        XCTAssertEqual(t.mode, .relay)
        XCTAssertEqual(t.matchBundleIds, [String]())
        XCTAssertTrue(t.enabled)
        XCTAssertEqual(t.relayURL, relayURL)
        XCTAssertEqual(t.deviceID, "dev-mac")
        XCTAssertEqual(t.deviceToken, "mac-tok")
        XCTAssertEqual(t.targetDeviceID, "dev-win")
        XCTAssertTrue(t.isValid)
    }

    func testEmptyPeersYieldsEmpty() {
        let targets = RelayTargetSynthesis.targets(
            peers: [], localDeviceID: "dev-mac", deviceToken: "t", relayURL: relayURL)
        XCTAssertTrue(targets.isEmpty)
    }
}
