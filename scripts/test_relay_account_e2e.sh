#!/bin/bash
# End-to-end smoke for the self-service account layer:
# register -> register a receiver device (session auth) -> register a sender
# device -> GET /v1/devices -> dispatch sender->receiver -> verify clipboard.
# macOS dev machine only.

set -euo pipefail
cd "$(dirname "$0")/.."

for tool in jq openssl curl; do
    command -v "$tool" >/dev/null 2>&1 || { echo "SKIP: missing required tool: $tool"; exit 0; }
done

TMP=$(mktemp -d)
SAVED_CLIP=$(pbpaste 2>/dev/null) || true
ADMIN="admin-$(openssl rand -hex 8)"
INVITE="invite-$(openssl rand -hex 4)"
SESSION_KEY="sk-$(openssl rand -hex 16)"
RELAY_PID=""
SUB_PID=""
BASE="http://127.0.0.1:8443"

cleanup() {
    [ -n "$RELAY_PID" ] && kill "$RELAY_PID" 2>/dev/null || true
    [ -n "$SUB_PID" ] && kill "$SUB_PID" 2>/dev/null || true
    rm -rf "$TMP"
    echo "$SAVED_CLIP" | pbcopy || true
}
trap cleanup EXIT

make -C relay build-darwin >/dev/null
make -C receiver build-darwin-arm64 >/dev/null
RELAY=./relay/dist/type4me-relay-darwin-arm64
RECV=./receiver/dist/type4me-receiver-darwin-arm64

# 1. Start relay with invite code + session key
TYPE4ME_RELAY_ADMIN_TOKEN="$ADMIN" \
TYPE4ME_RELAY_STATE_DIR="$TMP/state" \
TYPE4ME_RELAY_INVITE_CODES="$INVITE" \
TYPE4ME_RELAY_SESSION_KEY="$SESSION_KEY" \
"$RELAY" serve > "$TMP/relay.log" 2>&1 &
RELAY_PID=$!
for i in $(seq 1 30); do
    curl -fs "$BASE/healthz" >/dev/null 2>&1 && break
    sleep 0.1
done

# 2. Register a user (self-service)
SESSION=$(curl -sf -X POST -H "Content-Type: application/json" \
    -d "{\"username\":\"e2euser\",\"password\":\"supersecret\",\"invite_code\":\"$INVITE\"}" \
    "$BASE/v1/auth/register" | jq -r .session_token)
[ -n "$SESSION" ] && [ "$SESSION" != "null" ] || { echo "FAIL: no session token"; cat "$TMP/relay.log"; exit 1; }

# 3. Register receiver (Win) + sender (Mac) via session auth
WIN=$(curl -sf -X POST -H "Authorization: Bearer $SESSION" \
    -H "Content-Type: application/json" -d '{"label":"Win"}' "$BASE/v1/devices")
MAC=$(curl -sf -X POST -H "Authorization: Bearer $SESSION" \
    -H "Content-Type: application/json" -d '{"label":"Mac"}' "$BASE/v1/devices")
WIN_ID=$(echo "$WIN" | jq -r .device_id)
WIN_TOK=$(echo "$WIN" | jq -r .device_token)
MAC_TOK=$(echo "$MAC" | jq -r .device_token)
for v in "$WIN_ID" "$WIN_TOK" "$MAC_TOK"; do
    [ -n "$v" ] && [ "$v" != "null" ] || { echo "FAIL: device registration returned empty id/token"; echo "WIN=$WIN"; echo "MAC=$MAC"; cat "$TMP/relay.log"; exit 1; }
done

# 4. GET /v1/devices should list both
COUNT=$(curl -sf -H "Authorization: Bearer $SESSION" "$BASE/v1/devices" | jq '.devices | length')
[ "$COUNT" = "2" ] || { echo "FAIL: device count=$COUNT want 2"; exit 1; }

# 5. Start subscriber as the receiver device
TYPE4ME_MODE=relay-subscriber \
TYPE4ME_RELAY_URL="$BASE" \
TYPE4ME_DEVICE_ID="$WIN_ID" \
TYPE4ME_DEVICE_TOKEN="$WIN_TOK" \
"$RECV" --config "$TMP/recv.json" > "$TMP/subscriber.log" 2>&1 &
SUB_PID=$!
for i in $(seq 1 50); do
    grep -q "connected" "$TMP/subscriber.log" 2>/dev/null && break
    sleep 0.1
done

# 6. Dispatch sender -> receiver
TEXT="account e2e $(date +%s)"
curl -sf -X POST -H "Authorization: Bearer $MAC_TOK" \
    -H "Content-Type: application/json" \
    -d "{\"target_device_id\":\"$WIN_ID\",\"text\":\"$TEXT\"}" \
    "$BASE/v1/dispatch" >/dev/null
sleep 0.5

GOT=$(pbpaste)
if [ "$GOT" = "$TEXT" ]; then
    echo "PASS: self-service register -> device -> list -> dispatch works"
    exit 0
else
    echo "FAIL: clipboard='$GOT' expected='$TEXT'"
    echo "--- relay log ---"; cat "$TMP/relay.log"
    echo "--- subscriber log ---"; cat "$TMP/subscriber.log"
    exit 1
fi
