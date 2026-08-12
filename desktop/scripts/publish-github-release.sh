#!/usr/bin/env bash
set -euo pipefail

release_tag=${1:-}
asset_dir=${2:-}
notes_file=${3:-}
if [[ -z "$release_tag" || ! -d "$asset_dir" || ! -f "$notes_file" ]]; then
  echo "Usage: publish-github-release.sh <release-tag> <asset-dir> <notes-file>" >&2
  exit 2
fi

if gh release view "$release_tag" >/dev/null 2>&1; then
  gh release upload "$release_tag" "$asset_dir"/* --clobber
  desired_assets=$(mktemp)
  find "$asset_dir" -maxdepth 1 -type f -exec basename {} \; | sort > "$desired_assets"
  while IFS= read -r asset; do
    if [[ -n "$asset" ]] && ! grep -Fqx "$asset" "$desired_assets"; then
      gh release delete-asset "$release_tag" "$asset" --yes
    fi
  done < <(gh release view "$release_tag" --json assets --jq '.assets[].name')
  gh release edit "$release_tag" --title "easy-stock $release_tag" --notes-file "$notes_file"
else
  gh release create "$release_tag" "$asset_dir"/* --verify-tag --title "easy-stock $release_tag" --notes-file "$notes_file"
fi

expected=$(find "$asset_dir" -maxdepth 1 -type f | wc -l | tr -d ' ')
actual=$(gh release view "$release_tag" --json assets --jq '.assets | length')
[[ "$actual" == "$expected" ]] || { echo "GitHub Release contains $actual assets; expected $expected" >&2; exit 1; }
echo "Published GitHub Release $release_tag with $actual user-facing assets"
