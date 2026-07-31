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

# Reject archive members that could escape the extraction directory:
# absolute paths and ".." or empty path components (e.g. "../x", "a//b").
# tar's built-in sanitization differs between implementations, so it must
# not be relied on.
reject_unsafe_members() {
  local name
  while IFS= read -r name; do
    [[ "$name" == /* || "$name" == *//* ]] && {
      echo "unsafe archive member: $name" >&2
      return 1
    }
    local part
    IFS=/ read -ra parts <<<"$name"
    for part in "${parts[@]}"; do
      [[ "$part" == ".." || -z "$part" ]] && {
        echo "unsafe archive member: $name" >&2
        return 1
      }
    done
  done
  return 0
}

# Reject symlink/hardlink entries: their link targets bypass member-name
# validation and can point outside the restore root (e.g. data/x -> /etc/...
# that the engine later reads or writes through). tar -t lists them with
# type character l/h in the first column.
reject_link_entries() {
  local line
  while IFS= read -r line; do
    case "$line" in
      [lh]*) echo "archive contains link entry: $line" >&2; return 1 ;;
    esac
  done
  return 0
}

if ! tar -tzf "$ARCHIVE" | reject_unsafe_members; then
  echo "archive rejected: unsafe member paths" >&2
  exit 1
fi
if ! tar -tvzf "$ARCHIVE" | reject_link_entries; then
  echo "archive rejected: link entries are not allowed" >&2
  exit 1
fi

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT
tar -C "$WORKDIR" -xzf "$ARCHIVE"

if [[ ! -d "$WORKDIR/data" ]]; then
  echo "archive does not contain data/ payload" >&2
  exit 1
fi

# Defense in depth: after extraction, no link or device entries may exist in
# the staging tree (guards against archives crafted with unusual encodings
# that listing-based checks cannot see).
if [[ -n "$(find "$WORKDIR" \( -type l -o -type b -o -type c \) -print)" ]]; then
  echo "extracted archive contains link or device entries" >&2
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
