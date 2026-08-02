#!/usr/bin/env bash
# scripts/make-demo-gif.sh — build the LSM Engine demo GIF
#
# Usage: bash scripts/make-demo-gif.sh
#
# Produces:  docs/assets/demo.gif   (and docs/assets/demo.mp4 for free)
# Requires:  ffmpeg, node (for playwright), go, bun
# The script starts the Go backend and BFF itself; stop them on exit.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FRONTEND_DIR="$REPO_ROOT/frontend"
FRAMES_DIR="/tmp/lsm-demo-frames"
OUT_GIF="$REPO_ROOT/docs/assets/demo.gif"
OUT_MP4="$REPO_ROOT/docs/assets/demo.mp4"
DATA_DIR="/tmp/lsm-demo-data"
PALETTE="/tmp/lsm-demo-palette.png"
SERVER_BIN="/tmp/lsm-demo-server"
GIF_WIDTH=1280   # resize to this width in the GIF (keeps aspect ratio)
GIF_FPS=12

# ─── colour helpers ─────────────────────────────────────────────────────────
green() { printf '\033[32m%s\033[0m\n' "$*"; }
blue()  { printf '\033[34m%s\033[0m\n' "$*"; }
red()   { printf '\033[31m%s\033[0m\n' "$*"; }

# ─── cleanup on exit ────────────────────────────────────────────────────────
SERVER_PID=""
BFF_PID=""
cleanup() {
  [[ -n "$BFF_PID"    ]] && kill "$BFF_PID"    2>/dev/null || true
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  rm -f "$PALETTE"
}
trap cleanup EXIT

# ─── 1. build backend ───────────────────────────────────────────────────────
blue "▸ Building Go backend…"
cd "$REPO_ROOT"
go build -o "$SERVER_BIN" ./cmd/server/main.go

# ─── 2. seed data directory (use existing if present) ───────────────────────
mkdir -p "$DATA_DIR"
blue "▸ Starting Go backend (data → $DATA_DIR)…"
DATA_DIR="$DATA_DIR" \
  "$SERVER_BIN" &
SERVER_PID=$!

# wait for health
for i in {1..20}; do
  curl -sf http://localhost:8080/health > /dev/null 2>&1 && break
  sleep 0.5
done
green "  backend ready"

# ─── 3. seed interesting data so the UI looks live ──────────────────────────
blue "▸ Seeding demo data…"
for i in $(seq 1 60); do
  curl -s -X POST http://localhost:8080/db/put \
    -H 'Content-Type: application/json' \
    -d "{\"key\":\"metric:node1:cpu:$i\",\"value\":\"$(python3 -c "import random; print(round(random.uniform(10,95),2))")\"}" \
    > /dev/null
done

curl -s -X POST http://localhost:8080/db/batch \
  -H 'Content-Type: application/json' \
  -d '{
    "entries": [
      {"key":"config:compaction","value":"leveled","delete":false},
      {"key":"config:bloom_fpr","value":"0.01","delete":false},
      {"key":"config:block_size","value":"4096","delete":false},
      {"key":"config:l0_limit","value":"4","delete":false},
      {"key":"metric:node1:cpu:3","value":"","delete":true},
      {"key":"metric:node1:cpu:7","value":"","delete":true},
      {"key":"status:cluster","value":"standalone","delete":false},
      {"key":"status:wal","value":"active","delete":false}
    ]
  }' > /dev/null

# a few reads so cache stats show something
for k in "metric:node1:cpu:1" "metric:node1:cpu:10" "config:compaction" "status:cluster"; do
  curl -s "http://localhost:8080/db/get?key=$k" > /dev/null
done
green "  data seeded"

# ─── 4. build + start BFF ───────────────────────────────────────────────────
blue "▸ Building frontend…"
cd "$FRONTEND_DIR"
SKIP_CLIENT_BUILD="" bun run build:client > /dev/null 2>&1

blue "▸ Starting BFF…"
BACKEND_URL=http://localhost:8080 bun run index.ts > /tmp/lsm-bff.log 2>&1 &
BFF_PID=$!

for i in {1..20}; do
  curl -sf http://localhost:3001/ > /dev/null 2>&1 && break
  sleep 0.5
done
green "  BFF ready"

# ─── 5. capture frames ──────────────────────────────────────────────────────
blue "▸ Capturing frames…"
rm -rf "$FRAMES_DIR"
node "$REPO_ROOT/scripts/capture-demo.mjs" "$FRAMES_DIR"
FRAME_COUNT=$(ls "$FRAMES_DIR"/*.png 2>/dev/null | wc -l | tr -d ' ')
green "  $FRAME_COUNT frames captured"

# ─── 6. stop servers (cleanup trap handles this, but do it now) ─────────────
kill "$BFF_PID"    2>/dev/null || true;  BFF_PID=""
kill "$SERVER_PID" 2>/dev/null || true;  SERVER_PID=""

# ─── 7. build MP4 first (fast, lossless quality reference) ──────────────────
blue "▸ Encoding demo.mp4…"
ffmpeg -y -loglevel error \
  -framerate "$GIF_FPS" \
  -pattern_type glob -i "$FRAMES_DIR/frame_*.png" \
  -vf "scale=${GIF_WIDTH}:-2:flags=lanczos" \
  -c:v libx264 -crf 18 -preset fast -pix_fmt yuv420p \
  "$OUT_MP4"
green "  $(du -sh "$OUT_MP4" | cut -f1)  →  $OUT_MP4"

# ─── 8. build GIF (two-pass palette for quality) ────────────────────────────
blue "▸ Generating colour palette…"
ffmpeg -y -loglevel error \
  -framerate "$GIF_FPS" \
  -pattern_type glob -i "$FRAMES_DIR/frame_*.png" \
  -vf "fps=${GIF_FPS},scale=${GIF_WIDTH}:-1:flags=lanczos,palettegen=stats_mode=diff" \
  "$PALETTE"

blue "▸ Encoding demo.gif…"
ffmpeg -y -loglevel error \
  -framerate "$GIF_FPS" \
  -pattern_type glob -i "$FRAMES_DIR/frame_*.png" \
  -i "$PALETTE" \
  -lavfi "fps=${GIF_FPS},scale=${GIF_WIDTH}:-1:flags=lanczos [x]; [x][1:v] paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
  "$OUT_GIF"
green "  $(du -sh "$OUT_GIF" | cut -f1)  →  $OUT_GIF"

# ─── 9. summary ─────────────────────────────────────────────────────────────
echo ""
green "✓ Demo assets ready:"
echo "  GIF  →  $OUT_GIF"
echo "  MP4  →  $OUT_MP4"
echo ""
echo "Add to README:"
echo '  <p align="center"><img width="860" src="docs/assets/demo.gif" /></p>'
echo ""
echo "MP4 link (for large-file note):"
echo "  [▶ Full video](docs/assets/demo.mp4)"
