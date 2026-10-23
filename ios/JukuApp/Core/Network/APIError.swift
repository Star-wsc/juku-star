import Foundation

/// API 错误（服务端 error 字段优先）
struct JukuAPIError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
    init(_ message: String) { self.message = message }
    init(from body: ApiErrorBody?) {
        self.message = body?.error ?? body?.code ?? "请求失败"
    }
}

extension ApiErrorBody: LocalizedError {
    var errorDescription: String? { error ?? code }
}
