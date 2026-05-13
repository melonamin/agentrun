#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-$ROOT/dist}"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

platforms=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
)

for platform in "${platforms[@]}"; do
  read -r goos goarch <<<"$platform"
  name="agentrun_${VERSION}_${goos}_${goarch}"
  out_dir="$DIST_DIR/$name"
  mkdir -p "$out_dir"

  binary="agentrun"
  if [[ "$goos" == "windows" ]]; then
    binary="agentrun.exe"
  fi

  echo "building $name"
  (
    cd "$ROOT"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
      -trimpath \
      -ldflags "-s -w" \
      -o "$out_dir/$binary" \
      ./cmd/agentrun
  )

  cp "$ROOT/README.md" "$out_dir/README.md"

  if [[ "$goos" == "windows" ]]; then
    (cd "$DIST_DIR" && zip -qr "$name.zip" "$name")
  else
    (cd "$DIST_DIR" && tar -czf "$name.tar.gz" "$name")
  fi
  rm -rf "$out_dir"
done

(
  cd "$DIST_DIR"
  sha256sum *.tar.gz > checksums.txt
)
