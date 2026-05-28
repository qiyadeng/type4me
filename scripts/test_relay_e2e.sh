#!/bin/bash
# End-to-end smoke for relay: start relay binary + receiver in subscriber mode,
# curl dispatch, verify clipboard. macOS dev machine only.

set -euo pipefail
cd "$(dirname "$0")/.."

TMP=$(mktemp -d)
SAVED_CLIP=$(pbpaste || true)
ADMIN="admin-$(openssl rand -hex 8)"
RELAY_PID=""
SUB_PID=""

cleanup() {
    [ -n "$RELAY_PID" ] && kill "$RELAY_PID" 2>/dev/null || true
    [ -n "$SUB_PID" ] && kill "$SUB_PID" 2>/dev/null || true
    rm -rf "$TMP"
    echo "$SAVED_CLIP" | pbcopy || true
}
trap cleanup EXIT

# Build both binaries
make -C relay build-darwin >/dev/null
make -C receiver build-darwin-arm64 >/dev/null

RELAY=./relay/dist/type4me-relay-darwin-arm64
RECV=./receiver/dist/type4me-receiver-darwin-arm64

# 1. Start relay
TYPE4ME_RELAY_ADMIN_TOKEN="$ADMIN" \
TYPE4ME_RELAY_STATE_DIR="$TMP/state" \
"$RELAY" serve > "$TMP/relay.log" 2>&1 &
RELAY_PID=$!

for i in $(seq 1 30); do
    curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break
    sleep 0.1
done

# 2. Create account + 2 devices
ACCT=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN" \
    -H "Content-Type: application/json" \
    -d '{"name":"E2E"}' http://127.0.0.1:8443/v1/admin/accounts | jq -r .account_id)
MAC=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN" \
    -H "Content-Type: application/json" \
    -d "{\"account_id\":\"$ACCT\",\"label\":\"Mac\"}" \
    http://127.0.0.1:8443/v1/admin/devices)
WIN=$(curl -sf -X POST -H "Authorization: Bearer $ADMIN" \
    -H "Content-Type: application/json" \
    -d "{\"account_id\":\"$ACCT\",\"label\":\"Win\"}" \
    http://127.0.0.1:8443/v1/admin/devices)
MAC_TOK=$(echo "$MAC" | jq -r .device_token)
WIN_ID=$(echo "$WIN" | jq -r .device_id)
WIN_TOK=$(echo "$WIN" | jq -r .device_token)

# 3. Start subscriber (point config at TMP so we don't touch user state)
TYPE4ME_MODE=relay-subscriber \
TYPE4ME_RELAY_URL=http://127.0.0.1:8443 \
TYPE4ME_DEVICE_ID="$WIN_ID" \
TYPE4ME_DEVICE_TOKEN="$WIN_TOK" \
"$RECV" --config "$TMP/recv.json" > "$TMP/subscriber.log" 2>&1 &
SUB_PID=$!
sleep 1

# 4. Dispatch
TEXT="relay e2e $(date +%s)"
RESP=$(curl -sf -X POST -H "Authorization: Bearer $MAC_TOK" \
    -H "Content-Type: application/json" \
    -d "{\"target_device_id\":\"$WIN_ID\",\"text\":\"$TEXT\"}" \
    http://127.0.0.1:8443/v1/dispatch)
echo "Dispatch response: $RESP"

sleep 0.5
GOT=$(pbpaste)

if [ "$GOT" = "$TEXT" ]; then
    echo "PASS: clipboard contains expected text"
    exit 0
else
    echo "FAIL: clipboard='$GOT' expected='$TEXT'"
    echo "--- relay log ---"; cat "$TMP/relay.log"
    echo "--- subscriber log ---"; cat "$TMP/subscriber.log"
    exit 1
fi
