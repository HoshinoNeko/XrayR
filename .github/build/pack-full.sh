#!/usr/bin/env sh
set -eu
# Always operate on a temporary copy; preserve the original on unsupported targets.
binary=$1
stage=$(mktemp -d)
packed="$stage/packed"
if upx --best --lzma -o "$packed" "$binary"; then
  upx -t "$packed"
  mv "$packed" "$binary"
else
  echo "::warning::UPX cannot pack $binary on this target; retaining the original executable." >&2
fi
