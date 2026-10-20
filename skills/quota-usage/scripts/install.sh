#!/usr/bin/env bash
# Install the quota CLI from GitHub releases into ~/.local/bin (override with QUOTA_BIN_DIR).
# Pin a version with QUOTA_VERSION=v0.1.0; the default is the latest release.
set -euo pipefail

repo="wuhanhao456/quota-cli"
bin_dir="${QUOTA_BIN_DIR:-$HOME/.local/bin}"
version="${QUOTA_VERSION:-latest}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*) os=windows ;;
  *) echo "unsupported OS: $(uname -s); build with: go install github.com/$repo/cmd/quota@latest" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

ext=""
[ "$os" = windows ] && ext=".exe"
asset="quota-$os-$arch$ext"
if [ "$version" = latest ]; then
  url="https://github.com/$repo/releases/latest/download/$asset"
else
  url="https://github.com/$repo/releases/download/$version/$asset"
fi

mkdir -p "$bin_dir"
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
echo "downloading $url"
if ! curl -fsSL "$url" -o "$tmp"; then
  echo "download failed; if no release exists yet, build with: go install github.com/$repo/cmd/quota@latest" >&2
  exit 1
fi
install -m 0755 "$tmp" "$bin_dir/quota$ext"
echo "installed $("$bin_dir/quota$ext" --version) to $bin_dir/quota$ext"
case ":$PATH:" in
  *":$bin_dir:"*) ;;
  *) echo "note: $bin_dir is not on PATH" ;;
esac
