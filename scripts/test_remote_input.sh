#!/bin/bash
# End-to-end smoke: start receiver on loopback, POST text via curl, verify
# the clipboard contains the text. Does NOT exercise the Mac-side ASR/Type4Me
# UI — pure receiver + HTTP transport sanity check.

set -euo pipefail

cd "$(dirname "$0")/.."

PORT=47318
TOKEN="smoke-token-$(date +%s)"
TEXT="你好 type4me $(date +%H%M%S)"

# Save current clipboard so we can restore it
SAVED_CLIP=$(pbpaste || true)
trap 'echo "$SAVED_CLIP" | pbcopy || true' EXIT

# Build if needed
( cd receiver && make build-darwin-arm64 >/dev/null 2>&1 )

BIN="receiver/dist/type4me-receiver-darwin-arm64"

# Start receiver
TYPE4ME_TOKEN="$TOKEN" TYPE4ME_PORT="$PORT" "$BIN" >/tmp/t4m-receiver.log 2>&1 &
RX_PID=$!
trap "kill $RX_PID 2>/dev/null || true; echo \"$SAVED_CLIP\" | pbcopy || true" EXIT

# Wait until /ping is up
for i in $(seq 1 20); do
    if curl -fs "http://127.0.0.1:$PORT/ping" >/dev/null 2>&1; then break; fi
    sleep 0.1
done

# Send inject
RESP=$(curl -s -X POST "http://127.0.0.1:$PORT/inject" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"text\":\"$TEXT\"}")

echo "Receiver response: $RESP"

# Give it a beat to settle clipboard
sleep 0.3
GOT=$(pbpaste)

if [ "$GOT" = "$TEXT" ]; then
    echo "PASS: clipboard contains expected text"
    exit 0
else
    echo "FAIL: clipboard='$GOT' expected='$TEXT'"
    cat /tmp/t4m-receiver.log
    exit 1
fi
