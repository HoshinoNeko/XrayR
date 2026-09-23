#!/usr/bin/env bash
set -euo pipefail

: "${ASSET_NAME:?ASSET_NAME must identify the release platform}"
mkdir -p upx_artifacts
report="$PWD/upx_artifacts/UPX.txt"
upx --version > "$report"
printf '\nFlags: --best --lzma\nExperimental copies; target-platform runtime compatibility is not guaranteed.\n' >> "$report"

for suffix in ""; do
  name="XrayR-$ASSET_NAME$suffix"
  source_dir="$PWD/$name"
  stage=$(mktemp -d)
  cp -R "$source_dir" "$stage/$name"
  ok=true
  found=false
  for binary in XrayR XrayR.exe XrayR_softfloat; do
    original="$source_dir/$binary"
    [ -f "$original" ] || continue
    found=true
    packed="$stage/$binary.upx"
    # Do not force unsupported formats, and never label an unpacked fallback as UPX.
    if upx -t "$original" >> "$report" 2>&1; then
      printf '\nAlready packed: %s\n' "$original" >> "$report"
    elif upx --best --lzma -o "$packed" "$original" >> "$report" 2>&1 &&
       upx -t "$packed" >> "$report" 2>&1; then
      mv "$packed" "$stage/$name/$binary"
    else
      ok=false
      printf '\nSkipped %s: packing or integrity check failed for %s.\n' "$name" "$binary" >> "$report"
      echo "::warning::UPX copy skipped for $name ($binary); normal release is unchanged."
      break
    fi
  done
  if [ "$ok" = true ] && [ "$found" = true ]; then
    archive="$name-upx.tar.gz"
    tar -czf "upx_artifacts/$archive" -C "$stage" "$name"
    (cd upx_artifacts && sha256sum "$archive" > "$archive.sha256")
    printf '\nCreated %s (UPX integrity checked, not runtime validated).\n' "$archive" >> "$report"
  else
    printf '\nNo compressed archive produced for %s.\n' "$name" >> "$report"
  fi
done

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf '\n### UPX Actions-only copies\n\nSee the UPX artifact report for compressed or skipped editions. Normal Release files are unchanged.\n' >> "$GITHUB_STEP_SUMMARY"
fi
