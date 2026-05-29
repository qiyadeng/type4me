import Foundation

/// Pure mapping from account peer devices to relay-mode OutputTargets.
/// Excludes this machine's own device. Account targets carry no matchBundleIds
/// (they never participate in .auto routing — the user picks them manually).
enum RelayTargetSynthesis {
    static func targets(peers: [RelayPeerDevice],
                        localDeviceID: String,
                        deviceToken: String,
                        relayURL: URL) -> [OutputTarget] {
        peers
            .filter { $0.id != localDeviceID }
            .map { peer in
                OutputTarget(
                    id: peer.id,
                    name: peer.label,
                    matchBundleIds: [],
                    enabled: true,
                    mode: .relay,
                    relayURL: relayURL,
                    deviceID: localDeviceID,
                    deviceToken: deviceToken,
                    targetDeviceID: peer.id
                )
            }
    }
}
