import SwiftUI

/// 登录/连接整页 — 对应 Android AuthScreen + iOS 文档 §7.1
struct AuthScreen: View {
    @EnvironmentObject var app: AppSession

    @State private var host = Prefs.baseURLString
    @State private var port = ""
    @State private var mode = 0 // 0 login 1 register 2 guest
    @State private var username = ""
    @State private var password = ""
    @State private var errorText = ""
    @State private var busy = false

    var body: some View {
        ScrollView {
            VStack(spacing: 18) {
                VStack(spacing: 8) {
                    Text("短剧库")
                        .font(.largeTitle.bold())
                    Text("Juku Drama")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
                .padding(.top, 36)

                GroupBox {
                    VStack(spacing: 0) {
                        labeledField("地址") {
                            // 过程中不修剪末尾 /，保留 https:// 的 //
                            TextField("https://域名 或 http://IP", text: $host)
                                .textInputAutocapitalization(.never)
                                .autocorrectionDisabled()
                                .keyboardType(.URL)
                        }
                        Divider()
                        labeledField("端口") {
                            TextField("8998（https 可留空）", text: $port)
                                .keyboardType(.numberPad)
                        }
                    }
                }
                Text("支持 https://域名 或 http://IP:端口")
                    .font(.footnote)
                    .foregroundStyle(.secondary)

                Picker("模式", selection: $mode) {
                    Text("登录").tag(0)
                    Text("注册").tag(1)
                    Text("仅连接").tag(2)
                }
                .pickerStyle(.segmented)

                if mode != 2 {
                    GroupBox {
                        VStack(spacing: 0) {
                            labeledField("用户名") {
                                TextField("username", text: $username)
                                    .textInputAutocapitalization(.never)
                                    .autocorrectionDisabled()
                            }
                            Divider()
                            labeledField("密码") {
                                // 禁用自动更正/大写/智能标点；值不做 trim
                                SecureField("password", text: $password)
                                    .textInputAutocapitalization(.never)
                                    .autocorrectionDisabled()
                                    .textContentType(.password)
                            }
                        }
                    }
                    Text("密码原样传输，不 trim、不滤字符")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }

                if !errorText.isEmpty {
                    Text(errorText)
                        .font(.footnote)
                        .foregroundStyle(.red)
                }

                Button {
                    Task { await submit() }
                } label: {
                    if busy {
                        ProgressView()
                            .frame(maxWidth: .infinity)
                    } else {
                        Text(mode == 2 ? "连接并进入" : mode == 1 ? "注册并进入" : "登录并进入")
                            .frame(maxWidth: .infinity)
                    }
                }
                .buttonStyle(.borderedProminent)
                .disabled(busy)

                Button("使用演示说明：请填写真实服务器后连接") {}
                    .font(.footnote)
                    .foregroundStyle(.secondary)
            }
            .padding(20)
        }
    }

    @ViewBuilder
    private func labeledField<Content: View>(_ label: String, @ViewBuilder content: () -> Content) -> some View {
        HStack {
            Text(label)
                .frame(width: 64, alignment: .leading)
                .foregroundStyle(.secondary)
            content()
        }
        .padding(.vertical, 4)
    }

    private func submit() async {
        errorText = ""
        busy = true
        defer { busy = false }

        let base = host
        let p = port.trimmingCharacters(in: .whitespaces)
        var raw = base.trimmingCharacters(in: .whitespaces)
        // 禁止在输入过程中去掉 https:// 的 //
        if !raw.lowercased().hasPrefix("http") {
            if !p.isEmpty { raw = "http://" + raw } else { raw = "https://" + raw }
        }
        if !p.isEmpty, !raw.contains(":" + p) {
            while raw.hasSuffix("/") { raw.removeLast() }
            raw += ":" + p
        }
        guard APIClient.normalizeBaseURL(raw) != nil else {
            errorText = "服务器地址无效"
            return
        }

        app.applyBaseURL(raw)

        do {
            switch mode {
            case 2:
                try await app.connectGuest()
            case 1:
                guard !username.isEmpty, !password.isEmpty else {
                    errorText = "请输入用户名和密码"
                    return
                }
                try await app.register(username: username, password: password)
            default:
                guard !username.isEmpty, !password.isEmpty else {
                    errorText = "请输入用户名和密码"
                    return
                }
                try await app.login(username: username, password: password)
            }
            app.flash(mode == 2 ? "已连接" : "登录成功")
        } catch {
            let msg = (error as? ApiErrorBody)?.error
                ?? (error as? JukuAPIError)?.errorDescription
                ?? error.localizedDescription
            errorText = msg
        }
    }
}
