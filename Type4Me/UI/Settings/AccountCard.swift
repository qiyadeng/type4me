import SwiftUI

struct AccountCard: View, SettingsCardHelpers {
    @Environment(AppState.self) private var appState

    @State private var username = ""
    @State private var password = ""
    @State private var invite = ""
    @State private var registerMode = false

    var body: some View {
        settingsGroupCard(L("账号", "Account"), icon: "person.crop.circle") {
            switch appState.account.state {
            case .loggedOut, .loggingIn, .sessionExpired:
                loginForm
            case .loggedIn:
                loggedInView
            }
        }
    }

    private var isBusy: Bool { appState.account.state == .loggingIn }

    @ViewBuilder
    private var loginForm: some View {
        VStack(alignment: .leading, spacing: 10) {
            if appState.account.state == .sessionExpired {
                Text(L("会话已过期,请重新登录以刷新设备列表(已配置目标仍可使用)。",
                       "Session expired — log in again to refresh the device list (configured targets still work)."))
                    .font(.system(size: 12))
                    .foregroundStyle(TF.settingsAccentAmber)
            }
            TextField(L("用户名", "Username"), text: $username).textFieldStyle(.roundedBorder)
            SecureField(L("密码", "Password"), text: $password).textFieldStyle(.roundedBorder)
            if registerMode {
                TextField(L("邀请码", "Invite code"), text: $invite).textFieldStyle(.roundedBorder)
            }
            if let err = appState.account.lastError, !err.isEmpty {
                Text(err).font(.system(size: 12)).foregroundStyle(TF.settingsAccentRed)
            }
            HStack {
                Button(registerMode ? L("注册", "Register") : L("登录", "Log in")) {
                    Task {
                        if registerMode {
                            await appState.account.register(username: username, password: password, inviteCode: invite)
                        } else {
                            await appState.account.login(username: username, password: password)
                        }
                    }
                }
                .disabled(isBusy || username.isEmpty || password.isEmpty)

                Button(registerMode ? L("已有账号?去登录", "Have an account? Log in")
                                    : L("没有账号?去注册", "No account? Register")) {
                    registerMode.toggle()
                }
                .buttonStyle(.plain)
                .foregroundStyle(TF.settingsTextSecondary)
            }
        }
    }

    @ViewBuilder
    private var loggedInView: some View {
        VStack(alignment: .leading, spacing: 8) {
            accountRow(L("账号", "Account"), appState.account.username.isEmpty ? "—" : appState.account.username)
            Text(L("我的设备", "My devices")).font(.system(size: 12)).foregroundStyle(TF.settingsTextSecondary)
            if appState.account.peers.isEmpty {
                Text(L("(没有其它设备)", "(no other devices)"))
                    .font(.system(size: 12)).foregroundStyle(TF.settingsTextTertiary)
            } else {
                ForEach(appState.account.peers, id: \.id) { peer in
                    HStack(spacing: 6) {
                        Image(systemName: peer.online ? "circle.fill" : "circle")
                            .font(.system(size: 8))
                            .foregroundStyle(peer.online ? TF.settingsAccentGreen : TF.settingsTextTertiary)
                        Text(peer.label).font(.system(size: 12)).foregroundStyle(TF.settingsText)
                    }
                }
            }
            HStack(spacing: 8) {
                Button(L("刷新", "Refresh")) { Task { await appState.account.refreshDevices() } }
                Button(L("退出登录", "Log out")) { appState.account.logout() }
            }
        }
    }

    private func accountRow(_ label: String, _ value: String) -> some View {
        HStack(alignment: .top) {
            Text(label).font(.system(size: 12)).foregroundStyle(TF.settingsTextSecondary)
                .frame(width: 100, alignment: .leading)
            Text(value).font(.system(size: 12)).foregroundStyle(TF.settingsText).textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}
