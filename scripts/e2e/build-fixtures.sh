#!/usr/bin/env bash
# Build synthetic JPEG sequence fixtures for E2E capture tests.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
IMG_DIR="${ROOT}/testdata/e2e/images"
MANIFEST="${IMG_DIR}/manifest.json"

mkdir -p "${IMG_DIR}"

if ! command -v exiftool >/dev/null 2>&1; then
  echo "exiftool is required to build E2E image fixtures" >&2
  exit 1
fi
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "ffmpeg is required to build E2E image fixtures" >&2
  exit 1
fi

echo '{"images":{' > "${MANIFEST}.tmp"
first=1
for i in 1 2 3; do
  out=$(printf "%s/seq-%03d.jpg" "${IMG_DIR}" "${i}")
  hue=$((i * 40))
  ffmpeg -hide_banner -loglevel error \
    -f lavfi -i "color=c=0x$(printf '%02x%02x%02x' "${hue}" 80 160):s=64x64:d=0.04" \
    -frames:v 1 -update 1 "${out}" -y
  exiftool -overwrite_original -q -q \
    -UserComment="AviationWX-E2E:seq-${i}" \
    "${out}" >/dev/null
  sha=$(shasum -a 256 "${out}" | awk '{print $1}')
  if [[ "${first}" -eq 1 ]]; then
    first=0
  else
    echo ',' >> "${MANIFEST}.tmp"
  fi
  printf '"seq-%03d.jpg":"%s"' "${i}" "${sha}" >> "${MANIFEST}.tmp"
done
echo '}}' >> "${MANIFEST}.tmp"
mv "${MANIFEST}.tmp" "${MANIFEST}"
echo "Wrote ${IMG_DIR}/seq-001.jpg .. seq-003.jpg and ${MANIFEST}"
