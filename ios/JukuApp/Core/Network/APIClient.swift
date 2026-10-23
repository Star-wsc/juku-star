import Foundation

/// 网络层：URLSession + Codable
/// 必须踩对的坑（iOS 文档 §6）：
/// 1. Cookie 名动态（SHA-256）→ 用 HTTPCookieStorage 全量接受
/// 2. 每个鉴权请求带 X-Juku-Viewer
/// 3. Sec-Fetch-Site: none
/// 4. 账号类 JSON body 必填（{} 也行），空 body → 415
/// 5. JSON 禁 HTML 转义
actor APIClient {
    nonisolated(unsafe) static var sharedViewerId: String?

    /// 供 UI/下载层同步读取（跨 actor 边界仅读）
    nonisolated(unsafe) var baseURL: URL?
    private let session: URLSession

    init(session: URLSession = {
        let config = URLSessionConfiguration.default
        config.httpCookieStorage = .shared
        config.httpShouldSetCookies = true
        config.httpCookieAcceptPolicy = .always
        // ATS：开发/自签阶段允许明文（正式可在 Info.plist 按域名例外）
        return URLSession(configuration: config)
    }()) {
        self.session = session
    }

    // MARK: - URL 规范化：保留 https:// 双斜杠；过程中不吞末尾 /

    nonisolated static func normalizeBaseURL(_ raw: String) -> URL? {
        var s = raw.trimmingCharacters(in: .whitespaces)
        // 不要去掉用户正在输入的 https://
        if s.isEmpty { return nil }
        if !s.lowercased().hasPrefix("http://"), !s.lowercased().hasPrefix("https://") {
            // 裸 IP/域名
            if s.range(of: #"^\d+\.\d+\.\d+\.\d+$"#, options: .regularExpression) != nil {
                s = "http://" + s
            } else {
                s = "https://" + s
            }
        }
        // 仅去掉多余末尾 /
        while s.hasSuffix("/") { s.removeLast() }
        return URL(string: s)
    }

    // MARK: - Public

    func get<T: Decodable>(_ type: T.Type, path: String, query: [String: String] = [:]) async throws -> T {
        try await send(path: path, method: "GET", query: query, body: nil as Data?)
    }

    func post<T: Decodable>(
        _ type: T.Type,
        path: String,
        body: [String: Any],
        statusCode: Int? = nil,
        allowEmpty: Bool = false
    ) async throws -> T {
        let data = try JSONSerialization.data(withJSONObject: body, options: [])
        return try await send(path: path, method: "POST", query: [:], body: data, expectStatus: statusCode, allowEmpty: allowEmpty)
    }

    // MARK: - Core

    private func send<T: Decodable>(
        path: String,
        method: String,
        query: [String: String],
        body: Data?,
        expectStatus: Int? = nil,
        allowEmpty: Bool = false
    ) async throws -> T {
        guard let base = baseURL else {
            throw URLError(.badURL)
        }
        var comps = URLComponents(url: base.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty {
            comps.queryItems = query.map { URLQueryItem(name: $0.key, value: $0.value) }
        }
        guard let url = comps.url else { throw URLError(.badURL) }

        var req = URLRequest(url: url)
        req.httpMethod = method
        req.httpBody = body
        // JSON 禁 HTML 转义：JSONSerialization 默认即安全
        if body != nil || method == "POST" {
            req.setValue("application/json; charset=utf-8", forHTTPHeaderField: "Content-Type")
        }
        req.setValue("none", forHTTPHeaderField: "Sec-Fetch-Site")
        if let vid = Self.sharedViewerId {
            req.setValue(vid, forHTTPHeaderField: "X-Juku-Viewer")
        }

        let (data, resp) = try await session.data(for: req)
        guard let http = resp as? HTTPURLResponse else { throw URLError(.badServerResponse) }

        if let expectStatus, http.statusCode != expectStatus {
            throw try Self.decodeError(data) ?? URLError(.badServerResponse)
        }
        if http.statusCode >= 400 {
            throw try Self.decodeError(data) ?? URLError(.init(rawValue: http.statusCode))
        }
        if data.isEmpty {
            if allowEmpty {
                return try JSONDecoder().decode(T.self, from: Data("true".utf8))
            }
            throw URLError(.zeroByteResource)
        }
        return try JSONDecoder().decode(T.self, from: data)
    }

    private static func decodeError(_ data: Data) -> ApiErrorBody? {
        try? JSONDecoder().decode(ApiErrorBody.self, from: data)
    }

    /// 图片地址：一律走 /api/ui/image?url=
    nonisolated static func imageURL(raw: String, base: URL?) -> URL? {
        if raw.hasPrefix("/api/ui/image") {
            return base?.appendingPathComponent(raw)
        }
        guard let base else { return nil }
        var comps = URLComponents(url: base.appendingPathComponent(Endpoints.image), resolvingAgainstBaseURL: false)!
        comps.queryItems = [URLQueryItem(name: "url", value: raw)]
        return comps.url
    }
}

// JukuAPIError / ApiErrorBody: LocalizedError 见 APIError.swift
