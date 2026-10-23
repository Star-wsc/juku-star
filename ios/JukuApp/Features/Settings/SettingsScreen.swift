import SwiftUI

/// 设置：账号 / 主题 / 站源 / 播放偏好 / iOS 风险 / 退出
struct SettingsScreen: View {
    @EnvironmentObject var app: AppSession
    @State private var showChangePass = false
    @State private var showDisclaimer = false
    @State private var oldPass = ""
    @State private var newPass = ""

    var body: some View {
        NavigationStack {
            Form {
                Section("账号") {
                    LabeledContent("当前身份", value: app.account?.username ?? (app.baseURL != nil ? "匿名身份" : "未连接"))
                    LabeledContent("服务器", value: app.baseURLString.isEmpty ? "—" : app.baseURLString)
                    Button("修改密码", role: .none) { showChangePass = true }
                        .disabled(app.account == nil)
                    Button("退出登录", role: .destructive) {
                        Task { await app.logout() }
                    }
                }

                Section("外观") {
                    Picker("主题", selection: Binding(
                        get: { app.themeMode },
                        set: { app.setTheme($0) }
                    )) {
                        ForEach(ThemeMode.allCases, id: \.self) { m in
                            Text(m.label).tag(m)
                        }
                    }
                }

                Section("站源开关") {
                    ForEach(SourceGroup.all, id: \.self) { g in
                        Toggle(SourceGroup.label(of: g), isOn: Binding(
                            get: { app.enabledSources.contains(g) },
                            set: { _ in app.toggleSource(g) }
                        ))
                    }
                }

                Section("播放偏好") {
                    Picker("默认画质", selection: Binding(
                        get: { Prefs.defaultQuality },
                        set: { Prefs.defaultQuality = $0 }
                    )) {
                        ForEach(["流畅", "标清", "高清", "超清"], id: \.self) { Text($0) }
                    }
                    Stepper(value: Binding(
                        get: { Prefs.defaultSpeed },
                        set: { Prefs.defaultSpeed = $0 }
                    ), in: 0.5...3.0, step: 0.25) {
                        Text(String(format: "默认倍速 %.2gx", Prefs.defaultSpeed))
                    }
                    Toggle("自动下一集", isOn: Binding(
                        get: { Prefs.autoNext },
                        set: { Prefs.autoNext = $0 }
                    ))
                    Toggle("临近结束预缓存", isOn: Binding(
                        get: { Prefs.prefetch },
                        set: { Prefs.prefetch = $0 }
                    ))
                }

                Section("iOS 分发与风险") {
                    LabeledContent("分发方式", value: "自签名 / 侧载")
                    LabeledContent("自签有效期", value: "免费 7 天 / 付费 1 年")
                    LabeledContent("后台下载", value: "可能被杀，回前台续传")
                    LabeledContent("ATS", value: "需允许明文 HTTP")
                }

                Section("关于") {
                    LabeledContent("版本", value: "iOS 1.1.0")
                    Button("使用声明") { showDisclaimer = true }
                }
            }
            .navigationTitle("设置")
        }
        .alert("修改密码", isPresented: $showChangePass) {
            SecureField("当前密码", text: $oldPass)
            SecureField("新密码", text: $newPass)
            Button("取消", role: .cancel) {}
            Button("确定") {
                Task {
                    try? await app.changePassword(current: oldPass, new: newPass)
                    app.flash("密码已修改")
                    oldPass = ""; newPass = ""
                }
            }
        }
        .alert("使用声明", isPresented: $showDisclaimer) {
            Button("我已阅读并知晓", role: .cancel) {
                Prefs.disclaimerAccepted = true
            }
        } message: {
            Text("""
            本软件仅提供本地播放器与下载管理能力，不生产、不编辑、不上传任何影视内容。
            片源、剧集、封面、简介均来自您配置的第三方服务器。
            请确保合法使用内容并遵守当地法律法规。
            """)
        }
    }
}
