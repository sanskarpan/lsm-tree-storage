#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 ]]; then
  echo "usage: $0 <data-dir> [output-dir]" >&2
  exit 1
fi

DATA_DIR=$1
OUTPUT_DIR=${2:-./backups}

if [[ ! -d "$DATA_DIR" ]]; then
  echo "data directory not found: $DATA_DIR" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

TIMESTAMP=$(date -u +"%Y%m%dT%H%M%SZ")
BASENAME="lsm-backup-${TIMESTAMP}"
ARCHIVE_PATH="${OUTPUT_DIR}/${BASENAME}.tar.gz"
CHECKSUM_PATH="${ARCHIVE_PATH}.sha256"
WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT

mkdir -p "$WORKDIR/data"
cp -R "$DATA_DIR"/. "$WORKDIR/data"/

cat >"${WORKDIR}/metadata.json" <<JSON
{
  "created_at_utc": "${TIMESTAMP}",
  "source_data_dir": "$(cd "$DATA_DIR" && pwd)",
  "hostname": "$(hostname)",
  "config_path": "${CONFIG_PATH:-config.yaml}"
}
JSON

if [[ -f "${CONFIG_PATH:-config.yaml}" ]]; then
  cp "${CONFIG_PATH:-config.yaml}" "${WORKDIR}/config.yaml"
fi

tar -C "$WORKDIR" -czf "$ARCHIVE_PATH" .
(cd "$OUTPUT_DIR" && shasum -a 256 "$(basename "$ARCHIVE_PATH")" >"$(basename "$CHECKSUM_PATH")")

echo "backup_archive=$ARCHIVE_PATH"
echo "backup_checksum=$CHECKSUM_PATH"
