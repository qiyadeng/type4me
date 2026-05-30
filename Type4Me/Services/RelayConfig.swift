import Foundation

enum RelayConfig {
    /// 固化的 relay 服务地址,与 Windows receiver / CLI 同值。用户从不输入;
    /// 打包/部署时按需改这里。
    static let defaultRelayURL = URL(string: "https://oc10.gouruicm.com")!
}
