import XCTest
@testable import Type4Me

final class OutputTargetStoreTests: XCTestCase {
    private var tempDir: URL!

    override func setUpWithError() throws {
        tempDir = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent("OutputTargetStoreTests-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: tempDir, withIntermediateDirectories: true)
    }

    override func tearDownWithError() throws {
        try? FileManager.default.removeItem(at: tempDir)
    }

    func testLoadFromEmptyFileReturnsEmpty() {
        let file = tempDir.appendingPathComponent("credentials.json")
        try? "{}".write(to: file, atomically: true, encoding: .utf8)
        let store = OutputTargetStore(credentialsFile: file)
        XCTAssertEqual(store.load(), [])
    }

    func testLoadFromMissingFileReturnsEmpty() {
        let file = tempDir.appendingPathComponent("nonexistent.json")
        let store = OutputTargetStore(credentialsFile: file)
        XCTAssertEqual(store.load(), [])
    }

    func testLoadParsesTargetsArray() throws {
        let file = tempDir.appendingPathComponent("credentials.json")
        let json = #"""
        {
          "tf_remote_targets": [
            {
              "id": "win-pc", "name": "Win-PC", "host": "10.0.0.5", "port": 47318,
              "token": "tok", "matchBundleIds": ["com.microsoft.rdc.macos"], "enabled": true
            }
          ]
        }
        """#
        try json.write(to: file, atomically: true, encoding: .utf8)
        let store = OutputTargetStore(credentialsFile: file)
        let targets = store.load()
        XCTAssertEqual(targets.count, 1)
        XCTAssertEqual(targets.first?.id, "win-pc")
        XCTAssertEqual(targets.first?.matchBundleIds, ["com.microsoft.rdc.macos"])
    }

    func testLoadIgnoresMalformedEntries() throws {
        let file = tempDir.appendingPathComponent("credentials.json")
        let json = #"""
        {"tf_remote_targets": [{"name": "no-id"}, {"id": "ok", "name": "OK",
          "host": "1.1.1.1", "port": 80, "token": "t", "matchBundleIds": [], "enabled": true}]}
        """#
        try json.write(to: file, atomically: true, encoding: .utf8)
        let store = OutputTargetStore(credentialsFile: file)
        let targets = store.load()
        XCTAssertEqual(targets.map { $0.id }, ["ok"])
    }
}
