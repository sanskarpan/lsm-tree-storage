#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
RESTORE="$ROOT/scripts/restore.sh"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

# --- happy path: normal backup archive restores ---
mkdir -p "$TMP/src/data"
echo "v1" >"$TMP/src/data/a.sst"
echo "{}" >"$TMP/src/metadata.json"
tar -C "$TMP/src" -czf "$TMP/good.tar.gz" data metadata.json
"$RESTORE" "$TMP/good.tar.gz" "$TMP/out" >"$TMP/restore.out"
[[ "$(cat "$TMP/out/a.sst")" == "v1" ]] || fail "happy path: restored content differs"
grep -q "restored_metadata" "$TMP/restore.out" || fail "happy path: metadata not reported"

# --- traversal member ("../escape.txt") must be rejected ---
echo "escape" >"$TMP/escape-src.txt"
python3 -c "
import tarfile
with tarfile.open('$TMP/traversal.tar.gz', 'w:gz') as t:
    t.add('$TMP/escape-src.txt', arcname='../escape.txt')
"
mkdir -p "$TMP/out2"
if "$RESTORE" "$TMP/traversal.tar.gz" "$TMP/out2" >/dev/null 2>&1; then
  fail "traversal archive was accepted"
fi
[[ ! -e "$TMP/escape.txt" ]] || fail "traversal member escaped the restore root"
[[ ! -e "$TMP/out2/escape.txt" ]] || fail "traversal member was extracted"

# --- symlink member pointing outside must be rejected ---
python3 -c "
import tarfile
with tarfile.open('$TMP/sym.tar.gz', 'w:gz') as t:
    info = tarfile.TarInfo('data/ln')
    info.type = tarfile.SYMTYPE
    info.linkname = '/etc/passwd'
    t.addfile(info)
"
mkdir -p "$TMP/out3"
if "$RESTORE" "$TMP/sym.tar.gz" "$TMP/out3" >/dev/null 2>&1; then
  fail "symlink archive was accepted"
fi

# --- hardlink member with absolute linkname must be rejected ---
python3 -c "
import tarfile
with tarfile.open('$TMP/hard.tar.gz', 'w:gz') as t:
    info = tarfile.TarInfo('data/hl')
    info.type = tarfile.LNKTYPE
    info.linkname = '/etc/passwd'
    t.addfile(info)
"
mkdir -p "$TMP/out3b"
if "$RESTORE" "$TMP/hard.tar.gz" "$TMP/out3b" >/dev/null 2>&1; then
  fail "hardlink archive was accepted"
fi

# --- malicious archive with --force must not destroy the target ---
mkdir -p "$TMP/out4"
echo "keep" >"$TMP/out4/keep.txt"
if "$RESTORE" "$TMP/traversal.tar.gz" "$TMP/out4" --force >/dev/null 2>&1; then
  fail "traversal archive accepted with --force"
fi
[[ "$(cat "$TMP/out4/keep.txt")" == "keep" ]] || fail "--force cleanup ran on a rejected archive"

# --- archive without data/ payload must be rejected ---
python3 -c "
import tarfile
with tarfile.open('$TMP/nodata.tar.gz', 'w:gz') as t:
    info = tarfile.TarInfo('metadata.json')
    t.addfile(info)
"
mkdir -p "$TMP/out5"
if "$RESTORE" "$TMP/nodata.tar.gz" "$TMP/out5" >/dev/null 2>&1; then
  fail "archive without data/ was accepted"
fi

# --- checksum mismatch must be rejected ---
printf "deadbeef  %s\n" "$(basename "$TMP/good.tar.gz")" >"$TMP/good.tar.gz.sha256"
mkdir -p "$TMP/out6"
if "$RESTORE" "$TMP/good.tar.gz" "$TMP/out6" >/dev/null 2>&1; then
  fail "archive with bad checksum was accepted"
fi

echo "PASS: restore.sh hardening"
