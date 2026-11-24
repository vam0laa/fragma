#!/bin/sh
# fragma installer: downloads the latest release, verifies its checksum,
# installs it and starts it. Read it before you run it:
#   curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
set -eu

BASE="${FRAGMA_BASE_URL:-https://github.com/vam0laa/fragma/releases/latest/download}"

[ "$(id -u)" = 0 ] || { echo "fragma: run as root: curl -fsSL <url> | sudo sh" >&2; exit 1; }
command -v nft >/dev/null 2>&1 || { echo "fragma: nftables is required (Arch: pacman -S nftables, Debian/Ubuntu: apt install nftables)" >&2; exit 1; }
command -v systemctl >/dev/null 2>&1 || { echo "fragma: a systemd based system is required" >&2; exit 1; }

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "fragma: unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
file="fragma-linux-$arch.tar.gz"

fetch() { # url dest
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else echo "fragma: curl or wget is required" >&2; exit 1; fi
}

echo "downloading $file ..."
fetch "$BASE/$file" "$tmp/$file"
fetch "$BASE/SHA256SUMS" "$tmp/SHA256SUMS"

echo "verifying checksum ..."
( cd "$tmp" && grep " $file\$" SHA256SUMS | sha256sum -c - >/dev/null ) || { echo "fragma: checksum mismatch, aborting" >&2; exit 1; }

tar -xzf "$tmp/$file" -C "$tmp" --no-same-owner fragma
# FRAGMA_INSTALL_ARGS exists for testing, e.g. "--prefix /tmp/x --no-systemctl"
# shellcheck disable=SC2086
"$tmp/fragma" install ${FRAGMA_INSTALL_ARGS:-}
