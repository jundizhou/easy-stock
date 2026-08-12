#!/usr/bin/env bash
set -euo pipefail

asset_dir=${1:-}
target_uri=${2:-oss://easy-stock-fs/updates/desktop}
public_url=${3:-https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop}

if [[ ! -d "$asset_dir" ]]; then
  echo "Usage: publish-updater-oss.sh <asset-dir> [target-uri] [public-url]" >&2
  exit 2
fi
if command -v ossutil >/dev/null 2>&1; then
  ossutil_command=$(command -v ossutil)
elif [[ -n "${OSSUTIL_BIN:-}" && -x "$OSSUTIL_BIN" ]]; then
  ossutil_command=$OSSUTIL_BIN
else
  echo "ossutil is required" >&2
  exit 2
fi
ossutil_options=()
[[ -n "${OSS_ACCESS_KEY_ID:-}" ]] && ossutil_options+=(--access-key-id "$OSS_ACCESS_KEY_ID")
[[ -n "${OSS_ACCESS_KEY_SECRET:-}" ]] && ossutil_options+=(--access-key-secret "$OSS_ACCESS_KEY_SECRET")
[[ -n "${OSS_ENDPOINT:-}" ]] && ossutil_options+=(--endpoint "$OSS_ENDPOINT")
[[ -n "${OSS_REGION:-}" ]] && ossutil_options+=(--region "$OSS_REGION")

versioned=()
while IFS= read -r file; do
  name=$(basename "$file")
  case "$name" in
    latest-mac.yml|latest.yml) ;;
    *) versioned+=("$file") ;;
  esac
done < <(find "$asset_dir" -maxdepth 1 -type f -print | sort)

for file in "${versioned[@]}"; do
  name=$(basename "$file")
  "$ossutil_command" "${ossutil_options[@]}" cp "$file" "${target_uri%/}/$name" --force --cache-control "public,max-age=31536000,immutable"
done

# Publish mutable manifests last so clients cannot observe an incomplete version.
for name in latest-mac.yml latest.yml; do
  file="$asset_dir/$name"
  [[ -f "$file" ]] || { echo "Missing updater metadata: $name" >&2; exit 1; }
  "$ossutil_command" "${ossutil_options[@]}" cp "$file" "${target_uri%/}/$name" --force --cache-control "no-cache, no-store, must-revalidate"
done

for file in "$asset_dir"/*; do
  name=$(basename "$file")
  curl --fail --silent --show-error --location --head "${public_url%/}/$name" >/dev/null
done

echo "Published and verified updater assets at ${public_url%/}"
