#!/usr/bin/env bash
# Proves the emulator works with zero outbound network dependency.
# Run inside a network namespace that has only loopback.
set -euo pipefail

DATA_DIR="$(mktemp -d)"
PORT=4599
BIN=/tmp/azlocal-offline

cleanup() {
  set +e
  pkill -f "$BIN" >/dev/null 2>&1
  rm -rf "$DATA_DIR"
  rm -f "$BIN"
}
trap cleanup EXIT

echo "==> build"
go build -trimpath -o "$BIN" ./cmd/azlocal

echo "==> start (offline: no external network needed)"
AZLOCAL_DATA_DIR="$DATA_DIR" "$BIN" start --port "$PORT" --log-level warn &
sleep 2

echo "==> health"
curl -sf "http://127.0.0.1:$PORT/health" >/dev/null

echo "==> create container"
curl -sf -X PUT \
  "http://127.0.0.1:$PORT/devstoreaccount1/offline?restype=container" \
  -H "x-ms-version: 2023-11-03" \
  -o /dev/null

echo "==> list containers"
curl -sf "http://127.0.0.1:$PORT/devstoreaccount1/?comp=list" \
  -H "x-ms-version: 2023-11-03" | grep -q "offline"

echo "==> assert only loopback is bound"
if command -v ss >/dev/null; then
  ss -tlnp 2>/dev/null | grep ":$PORT" | grep -qv "127.0.0.1:$PORT" \
    && { echo "FAIL: bound to non-loopback"; exit 1; }
fi

echo "PASS: offline test"
