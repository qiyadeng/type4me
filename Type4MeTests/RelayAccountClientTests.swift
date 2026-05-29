import XCTest
@testable import Type4Me

final class RelayAccountClientTests: XCTestCase {
    private var server: TestHTTPServer!
    private var client: RelayAccountClient!

    override func setUp() {
        super.setUp()
        server = TestHTTPServer()
        server.start()
        let url = URL(string: "http://127.0.0.1:\(server.port)")!
        client = RelayAccountClient(relayURL: url, session: .shared)
    }
    override func tearDown() { server.stop(); server = nil; client = nil; super.tearDown() }

    func testLoginSuccess() async throws {
        server.respond { req in
            XCTAssertEqual(req.method, "POST")
            XCTAssertEqual(req.path, "/v1/auth/login")
            return TestHTTPResponse(status: 200, body: #"{"session_token":"sess-1","account_id":"acct-1","username":"alice","expires_at":"2030-01-01T00:00:00Z"}"#)
        }
        let sess = try await client.login(username: "alice", password: "supersecret")
        XCTAssertEqual(sess.token, "sess-1")
        XCTAssertEqual(sess.accountID, "acct-1")
        XCTAssertEqual(sess.username, "alice")
        XCTAssertNotNil(sess.expiresAt, "expires_at should parse (non-fractional RFC3339)")
    }

    func testRegisterSendsInviteCode() async throws {
        let box = BodyBox()
        server.respond { req in
            box.value = req.body
            return TestHTTPResponse(status: 201, body: #"{"session_token":"s","account_id":"a","username":"bob"}"#)
        }
        _ = try await client.register(username: "bob", password: "supersecret", inviteCode: "INVITE-9")
        XCTAssertTrue(box.value.contains("INVITE-9"), "invite_code should be in body: \(box.value)")
    }

    func testRegisterDeviceSetsBearer() async throws {
        let box = BodyBox()
        server.respond { req in
            box.value = req.headers["Authorization"] ?? ""
            return TestHTTPResponse(status: 201, body: #"{"device_id":"dev-1","device_token":"dtok","label":"Mac","role":"either"}"#)
        }
        let dev = try await client.registerDevice(session: "sess-1", label: "Mac", role: "either")
        XCTAssertEqual(box.value, "Bearer sess-1")
        XCTAssertEqual(dev.id, "dev-1")
        XCTAssertEqual(dev.token, "dtok")
    }

    func testListDevicesMapsOnline() async throws {
        server.respond { _ in
            TestHTTPResponse(status: 200, body: #"{"devices":[{"id":"d1","label":"Win","role":"either","online":true},{"id":"d2","label":"Mac","role":"either","online":false}]}"#)
        }
        let peers = try await client.listDevices(session: "sess-1")
        XCTAssertEqual(peers.count, 2)
        XCTAssertEqual(peers[0].id, "d1")
        XCTAssertEqual(peers[0].online, true)
        XCTAssertEqual(peers[1].online, false)
    }

    func testErrorMappingThrowsAPIError() async {
        server.respond { _ in TestHTTPResponse(status: 401, body: #"{"error":"invalid_credentials"}"#) }
        do {
            _ = try await client.login(username: "alice", password: "wrong")
            XCTFail("expected error")
        } catch let RelayAPIError.api(status, code, message) {
            XCTAssertEqual(status, 401)
            XCTAssertEqual(code, "invalid_credentials")
            XCTAssertFalse(message.isEmpty)
        } catch {
            XCTFail("expected RelayAPIError.api, got \(error)")
        }
    }
}

/// Thread-safe box for capturing request data from the server callback.
final class BodyBox: @unchecked Sendable {
    private let lock = NSLock()
    private var _v = ""
    var value: String {
        get { lock.lock(); defer { lock.unlock() }; return _v }
        set { lock.lock(); _v = newValue; lock.unlock() }
    }
}
