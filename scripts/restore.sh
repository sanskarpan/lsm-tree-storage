#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 || $# -gt 3 ]]; then
  echo "usage: $0 <backup-archive.tar.gz> <target-dir> [--force]" >&2
  exit 1
fi

ARCHIVE=$1
TARGET_DIR=$2
FORCE=${3:-}

if [[ ! -f "$ARCHIVE" ]]; then
  echo "archive not found: $ARCHIVE" >&2
  exit 1
fi

if [[ -e "$TARGET_DIR" && "$FORCE" != "--force" ]]; then
  if [[ -n "$(find "$TARGET_DIR" -mindepth 1 -maxdepth 1 2>/dev/null)" ]]; then
    echo "target directory is not empty: $TARGET_DIR (use --force to replace it)" >&2
    exit 1
  fi
fi

CHECKSUM_FILE="${ARCHIVE}.sha256"
if [[ -f "$CHECKSUM_FILE" ]]; then
  EXPECTED=$(awk '{print $1}' "$CHECKSUM_FILE")
  ACTUAL=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')
  if [[ "$EXPECTED" != "$ACTUAL" ]]; then
    echo "checksum mismatch for $ARCHIVE" >&2
    exit 1
  fi
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT
tar -C "$WORKDIR" -xzf "$ARCHIVE"

if [[ ! -d "$WORKDIR/data" ]]; then
  echo "archive does not contain data/ payload" >&2
  exit 1
fi

mkdir -p "$TARGET_DIR"
if [[ "$FORCE" == "--force" ]]; then
  find "$TARGET_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
fi

cp -R "$WORKDIR/data"/. "$TARGET_DIR"/

if [[ -f "$WORKDIR/metadata.json" ]]; then
  echo "restored_metadata:"
  cat "$WORKDIR/metadata.json"
fi

echo "restored_to=$(cd "$TARGET_DIR" && pwd)"
