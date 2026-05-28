import XCTest
@testable import Type4Me

final class OutputTargetTests: XCTestCase {
    func testCodableRoundTrip() throws {
        let original = OutputTarget(
            id: "win-pc",
            name: "Win-PC",
            host: "192.168.1.42",
            port: 47318,
            token: "abc123",
            matchBundleIds: ["com.microsoft.rdc.macos", "com.parsecgaming.parsec"],
            enabled: true
        )
        let data = try JSONEncoder().encode(original)
        let decoded = try JSONDecoder().decode(OutputTarget.self, from: data)
        XCTAssertEqual(decoded, original)
    }

    func testBaseURLConstruction() {
        let t = OutputTarget(id: "x", name: "X", host: "10.0.0.1", port: 47318,
                             token: "tok", matchBundleIds: [], enabled: true)
        XCTAssertEqual(t.baseURL.absoluteString, "http://10.0.0.1:47318")
    }

    func testIPv6HostBracketed() {
        let t = OutputTarget(id: "x", name: "X", host: "fd7a::1", port: 47318,
                             token: "tok", matchBundleIds: [], enabled: true)
        XCTAssertEqual(t.baseURL.absoluteString, "http://[fd7a::1]:47318")
    }

    func testCodableModeDefaultsToDirectWhenMissing() throws {
        let json = #"""
        {"id":"t","name":"T","enabled":true,"matchBundleIds":[],
         "host":"1.1.1.1","port":80,"token":"tok"}
        """#.data(using: .utf8)!
        let t = try JSONDecoder().decode(OutputTarget.self, from: json)
        XCTAssertEqual(t.mode, .direct)
        XCTAssertTrue(t.isValid)
    }

    func testCodableRelayMode() throws {
        let json = #"""
        {"id":"t","name":"T","enabled":true,"matchBundleIds":[],"mode":"relay",
         "relay_url":"https://relay.example.com","device_id":"dev-Mac",
         "device_token":"tok","target_device_id":"dev-Win"}
        """#.data(using: .utf8)!
        let t = try JSONDecoder().decode(OutputTarget.self, from: json)
        XCTAssertEqual(t.mode, .relay)
        XCTAssertEqual(t.deviceID, "dev-Mac")
        XCTAssertEqual(t.targetDeviceID, "dev-Win")
        XCTAssertTrue(t.isValid)
    }

    func testIsValidFailsForRelayMissingFields() throws {
        let json = #"""
        {"id":"t","name":"T","enabled":true,"matchBundleIds":[],"mode":"relay",
         "relay_url":"https://relay.example.com"}
        """#.data(using: .utf8)!
        let t = try JSONDecoder().decode(OutputTarget.self, from: json)
        XCTAssertFalse(t.isValid)
    }

    func testIsValidFailsForDirectMissingFields() throws {
        let json = #"""
        {"id":"t","name":"T","enabled":true,"matchBundleIds":[]}
        """#.data(using: .utf8)!
        let t = try JSONDecoder().decode(OutputTarget.self, from: json)
        XCTAssertEqual(t.mode, .direct)
        XCTAssertFalse(t.isValid)
    }
}
