import SwiftUI
import AppKit

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MARK: - Remote Settings Tab
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

struct RemoteSettingsTab: View, SettingsCardHelpers {

    @Environment(AppState.self) private var appState

    private var credentialsURL: URL { OutputTargetStore.defaultCredentialsFile }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            SettingsSectionHeader(
                label: L("远程", "Remote"),
                title: L("远程目标", "Remote Targets"),
                description: L(
                    "当前台 App 的 bundle id 与某个 target 的 matchBundleIds 匹配时,转写文本会路由到该 target(LAN 直连或 relay)。token 经过遮挡显示。手改 credentials.json 后点 \"重新加载\"。",
                    "When the frontmost app's bundle id matches a target's matchBundleIds, transcribed text is routed there (LAN direct or relay). Tokens are masked. Hand-edit credentials.json, then tap \"Reload\"."
                )
            )

            activeOutputCard

            credentialsCard

            if appState.remoteTargets.isEmpty {
                emptyState
            } else {
                ForEach(appState.remoteTargets) { target in
                    targetCard(target)
                }
            }
        }
    }

    // MARK: - Active output target

    private var activeOutputCard: some View {
        // Display-fallback: if the locked target was deleted/disabled, show
        // "Auto" rather than an empty radio group. Routing already degrades to
        // local in that case (see OutputRouter.resolve).
        let selection = Binding<OutputOverride>(
            get: {
                if case let .remote(id) = appState.outputOverride,
                   !appState.remoteTargets.contains(where: { $0.id == id && $0.enabled }) {
                    return .auto
                }
                return appState.outputOverride
            },
            set: { appState.outputOverride = $0 }
        )
        return settingsGroupCard(L("激活目标", "Active Output"), icon: "scope") {
            VStack(alignment: .leading, spacing: 8) {
                Text(L(
                    "锁定转写文本的去向。选「自动」时按前台 App 的 bundle id 匹配;选某台远程时,无视前台 App 直接发往该机器(适用于同一远程桌面客户端开多台时,自动匹配分不清的情况)。切换在下一次说话生效。",
                    "Lock where transcribed text goes. \"Auto\" matches by the frontmost app's bundle id; picking a remote sends there regardless of the frontmost app (use this when one remote-desktop client has several machines open and auto-match can't tell them apart). Takes effect on your next recording."
                ))
                .font(.system(size: 12))
                .foregroundStyle(TF.settingsTextSecondary)
                .frame(maxWidth: .infinity, alignment: .leading)

                Picker("", selection: selection) {
                    Text(L("自动(按前台 App 匹配)", "Auto (match frontmost app)")).tag(OutputOverride.auto)
                    Text(L("本机 Mac", "This Mac")).tag(OutputOverride.local)
                    ForEach(appState.remoteTargets.filter { $0.enabled }) { t in
                        Text(t.name).tag(OutputOverride.remote(t.id))
                    }
                }
                .labelsHidden()
                .pickerStyle(.radioGroup)
            }
        }
    }

    // MARK: - Credentials file path + actions

    private var credentialsCard: some View {
        settingsGroupCard(L("配置文件", "Config File"), icon: "doc.text") {
            VStack(alignment: .leading, spacing: 12) {
                Text(credentialsURL.path)
                    .font(.system(size: 12, design: .monospaced))
                    .foregroundStyle(TF.settingsTextSecondary)
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)

                HStack(spacing: 8) {
                    Button(L("在 Finder 中显示", "Reveal in Finder")) {
                        NSWorkspace.shared.activateFileViewerSelecting([credentialsURL])
                    }
                    Button(L("重新加载", "Reload")) {
                        appState.reloadRemoteTargets()
                    }
                }
            }
        }
    }

    // MARK: - Empty state

    private var emptyState: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(L("未配置任何远程目标。", "No remote targets configured."))
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(TF.settingsText)
            Text(L(
                "在 credentials.json 中加 \"tf_remote_targets\" 数组,然后点上面的 \"重新加载\"。",
                "Add a \"tf_remote_targets\" array to credentials.json, then tap \"Reload\" above."
            ))
            .font(.system(size: 12))
            .foregroundStyle(TF.settingsTextTertiary)
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10).fill(TF.settingsBg))
    }

    // MARK: - Per-target card

    @ViewBuilder
    private func targetCard(_ t: OutputTarget) -> some View {
        let trailing = AnyView(
            HStack(spacing: 6) {
                modePill(t.mode)
                statusBadge(valid: t.isValid, enabled: t.enabled)
            }
        )
        settingsGroupCard(t.name, icon: t.mode == .relay ? "antenna.radiowaves.left.and.right" : "network", trailing: trailing) {
            VStack(alignment: .leading, spacing: 0) {
                modeFields(t)

                SettingsDivider()

                row(L("匹配 Bundle IDs", "Match Bundle IDs"),
                    t.matchBundleIds.isEmpty
                        ? L("(无,该 target 不会被自动匹配)", "(none, won't auto-match)")
                        : t.matchBundleIds.joined(separator: "\n"),
                    multiline: true)

                row(L("启用", "Enabled"), t.enabled ? L("是", "Yes") : L("否", "No"))
                row(L("ID", "ID"), t.id)
            }
        }
    }

    @ViewBuilder
    private func modeFields(_ t: OutputTarget) -> some View {
        switch t.mode {
        case .direct:
            row(L("Host", "Host"), t.host ?? missing)
            row(L("Port", "Port"), t.port.map(String.init) ?? missing)
            row(L("Token", "Token"), maskToken(t.token))
        case .relay:
            row(L("Relay URL", "Relay URL"), t.relayURL?.absoluteString ?? missing)
            row(L("本机 Device ID", "Local Device ID"), t.deviceID ?? missing)
            row(L("目标 Device ID", "Target Device ID"), t.targetDeviceID ?? missing)
            row(L("Device Token", "Device Token"), maskToken(t.deviceToken))
        }
    }

    // MARK: - Small components

    private var missing: String { L("(未设置)", "(missing)") }

    private func modePill(_ mode: OutputTarget.Mode) -> some View {
        let label = mode == .relay ? "RELAY" : "DIRECT"
        let color: Color = mode == .relay ? TF.settingsAccentAmber : TF.settingsTextSecondary
        return Text(label)
            .font(.system(size: 9, weight: .semibold))
            .tracking(0.8)
            .foregroundStyle(color)
            .padding(.horizontal, 8)
            .padding(.vertical, 3)
            .background(
                RoundedRectangle(cornerRadius: 4)
                    .fill(color.opacity(0.15))
            )
    }

    private func statusBadge(valid: Bool, enabled: Bool) -> some View {
        let (sym, tint, label): (String, Color, String)
        if !valid {
            sym = "exclamationmark.triangle.fill"
            tint = TF.settingsAccentRed
            label = L("字段缺失", "Missing fields")
        } else if !enabled {
            sym = "pause.circle.fill"
            tint = TF.settingsTextTertiary
            label = L("已禁用", "Disabled")
        } else {
            sym = "checkmark.circle.fill"
            tint = TF.settingsAccentGreen
            label = L("就绪", "Ready")
        }
        return HStack(spacing: 4) {
            Image(systemName: sym)
                .font(.system(size: 11, weight: .semibold))
                .foregroundStyle(tint)
            Text(label)
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(tint)
        }
    }

    private func row(_ label: String, _ value: String, multiline: Bool = false) -> some View {
        HStack(alignment: .top) {
            Text(label)
                .font(.system(size: 12))
                .foregroundStyle(TF.settingsTextSecondary)
                .frame(width: 160, alignment: .leading)
            Text(value)
                .font(.system(size: 12, design: .monospaced))
                .foregroundStyle(TF.settingsText)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .multilineTextAlignment(.leading)
                .fixedSize(horizontal: false, vertical: multiline)
        }
        .padding(.vertical, 8)
    }

    /// Show first 4 + last 4 chars with middle masked. nil → "(missing)".
    private func maskToken(_ token: String?) -> String {
        guard let t = token, !t.isEmpty else { return missing }
        if t.count <= 8 { return String(repeating: "•", count: t.count) }
        let prefix = t.prefix(4)
        let suffix = t.suffix(4)
        return "\(prefix)••••••\(suffix)"
    }
}
