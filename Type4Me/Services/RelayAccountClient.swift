import Foundation

struct RelaySession: Equatable, Sendable {
    let token: String
    let accountID: String
    let username: String
    let expiresAt: Date?
}

struct RelayRegisteredDevice: Equatable, Sendable {
    let id: String
    let token: String
    let label: String
    let role: String
}

struct RelayPeerDevice: Equatable, Codable, Sendable {
    let id: String
    let label: String
    let online: Bool
}

enum RelayAPIError: Error, Equatable {
    case api(status: Int, code: String, message: String)
    case malformedResponse
    case transport(String)
}

extension RelayAPIError: LocalizedError {
    var errorDescription: String? {
        switch self {
        case .api(_, _, let message): return message
        case .malformedResponse: return "服务器响应异常"
        case .transport(let s): return s
        }
    }
}

protocol RelayAccountClienting: Sendable {
    func login(username: String, password: String) async throws -> RelaySession
    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession
    func registerDevice(session: String, label: String, role: String) async throws -> RelayRegisteredDevice
    func listDevices(session: String) async throws -> [RelayPeerDevice]
}

final class RelayAccountClient: RelayAccountClienting, @unchecked Sendable {
    private let relayURL: URL
    private let session: URLSession

    init(relayURL: URL = RelayConfig.defaultRelayURL, session: URLSession = .shared) {
        self.relayURL = relayURL
        self.session = session
    }

    private static let messages: [String: String] = [
        "bad_json": "请求格式错误",
        "username_invalid": "用户名需为 3-32 个字符",
        "password_too_short": "密码至少 8 个字符",
        "password_too_long": "密码过长",
        "username_taken": "用户名已被占用",
        "registration_disabled": "该服务未开放注册",
        "invalid_invite": "邀请码无效",
        "invalid_credentials": "用户名或密码错误",
        "invalid_session": "登录已过期,请重新登录",
        "rate_limited": "尝试过于频繁,请稍后再试",
        "account_not_found": "账号不存在",
    ]
    /// Parses RFC3339 with or without fractional seconds.
    private static func parseISO8601(_ s: String) -> Date? {
        let withFrac = ISO8601DateFormatter()
        withFrac.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return withFrac.date(from: s) ?? plain.date(from: s)
    }

    private static func message(for code: String) -> String {
        if let m = messages[code] { return m }
        return code.isEmpty ? "请求失败" : "请求失败 (\(code))"
    }

    private func post(_ path: String, bearer: String?, body: [String: Any]) async throws -> Data {
        var req = URLRequest(url: relayURL.appendingPathComponent(path))
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let bearer { req.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization") }
        req.httpBody = try JSONSerialization.data(withJSONObject: body)
        return try await send(req)
    }

    private func get(_ path: String, bearer: String) async throws -> Data {
        var req = URLRequest(url: relayURL.appendingPathComponent(path))
        req.httpMethod = "GET"
        req.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization")
        return try await send(req)
    }

    private func send(_ req: URLRequest) async throws -> Data {
        let data: Data, resp: URLResponse
        do { (data, resp) = try await session.data(for: req) }
        catch { throw RelayAPIError.transport(error.localizedDescription) }
        guard let http = resp as? HTTPURLResponse else { throw RelayAPIError.malformedResponse }
        if (200..<300).contains(http.statusCode) { return data }
        let code = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["error"] as? String ?? ""
        throw RelayAPIError.api(status: http.statusCode, code: code, message: Self.message(for: code))
    }

    private func decodeSession(_ data: Data) throws -> RelaySession {
        guard let d = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let token = d["session_token"] as? String,
              let acct = d["account_id"] as? String,
              let user = d["username"] as? String
        else { throw RelayAPIError.malformedResponse }
        let exp = (d["expires_at"] as? String).flatMap(Self.parseISO8601)
        return RelaySession(token: token, accountID: acct, username: user, expiresAt: exp)
    }

    func login(username: String, password: String) async throws -> RelaySession {
        try decodeSession(try await post("v1/auth/login", bearer: nil,
            body: ["username": username, "password": password]))
    }

    func register(username: String, password: String, inviteCode: String) async throws -> RelaySession {
        try decodeSession(try await post("v1/auth/register", bearer: nil,
            body: ["username": username, "password": password, "invite_code": inviteCode]))
    }

    func registerDevice(session sessionToken: String, label: String, role: String) async throws -> RelayRegisteredDevice {
        var body: [String: Any] = ["label": label]
        if !role.isEmpty { body["role"] = role }
        let data = try await post("v1/devices", bearer: sessionToken, body: body)
        guard let d = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let id = d["device_id"] as? String,
              let token = d["device_token"] as? String
        else { throw RelayAPIError.malformedResponse }
        return RelayRegisteredDevice(id: id, token: token,
                                     label: d["label"] as? String ?? label,
                                     role: d["role"] as? String ?? role)
    }

    func listDevices(session sessionToken: String) async throws -> [RelayPeerDevice] {
        let data = try await get("v1/devices", bearer: sessionToken)
        guard let d = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let arr = d["devices"] as? [[String: Any]]
        else { throw RelayAPIError.malformedResponse }
        return arr.compactMap { e in
            guard let id = e["id"] as? String, let label = e["label"] as? String else { return nil }
            return RelayPeerDevice(id: id, label: label, online: (e["online"] as? Bool) ?? false)
        }
    }
}
