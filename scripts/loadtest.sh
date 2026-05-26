#!/usr/bin/env bash
set -euo pipefail

TARGET=${TARGET:-http://127.0.0.1:3001}
DURATION=${DURATION:-30s}
CONCURRENCY=${CONCURRENCY:-8}
KEYSPACE=${KEYSPACE:-5000}
WRITE_PERCENT=${WRITE_PERCENT:-50}
VALUE_SIZE=${VALUE_SIZE:-128}

exec go run ./cmd/loadtest \
  -target "$TARGET" \
  -duration "$DURATION" \
  -concurrency "$CONCURRENCY" \
  -keyspace "$KEYSPACE" \
  -write-percent "$WRITE_PERCENT" \
  -value-size "$VALUE_SIZE" \
  -api-token "${API_TOKEN:-}" \
  -basic-auth "${BFF_BASIC_AUTH:-}"
