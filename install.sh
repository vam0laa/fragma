#!/bin/sh
# fragma installer: downloads the latest release, verifies its checksum,
# installs it and starts it. Read it before you run it:
#   curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
set -eu

BASE="${FRAGMA_BASE_URL:-https://github.com/vam0laa/fragma/releases/latest/download}"

# colors only on a terminal, and never when NO_COLOR is set
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  G='\033[38;5;114m'; R='\033[38;5;203m'; D='\033[38;5;244m'; B='\033[1m'; A='\033[38;5;111m'; N='\033[0m'; LIVE=1
else
  G=''; R=''; D=''; B=''; A=''; N=''; LIVE=''
fi

working() { [ -n "$LIVE" ] && printf '  %b…%b %s' "$D" "$N" "$1"; return 0; }
ok()      { [ -n "$LIVE" ] && printf '\r\033[K'; printf '  %b✔%b %b%-10s%b %b%s%b\n' "$G" "$N" "$B" "$1" "$N" "$D" "$2" "$N"; }
die()     { [ -n "$LIVE" ] && printf '\r\033[K'; printf '  %b✖%b %s\n' "$R" "$N" "$1" >&2; exit 1; }

printf '\n  %b%bfragma%b %binstaller%b\n  %bopen the sites your ISP blocks, with one command%b\n\n' "$B" "$A" "$N" "$B" "$N" "$D" "$N"

[ "$(id -u)" = 0 ] || die "run as root: curl -fsSL <url> | sudo sh"

working "Checking requirements"
command -v nft >/dev/null 2>&1 || die "nftables is required (Arch: pacman -S nftables, Debian/Ubuntu: apt install nftables)"
command -v systemctl >/dev/null 2>&1 || die "a systemd based system is required"
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported CPU: $(uname -m)" ;;
esac
ok "Checked" "nftables, systemd, $arch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
file="fragma-linux-$arch.tar.gz"

fetch() { # url dest
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else die "curl or wget is required"; fi
}

working "Downloading $file"
fetch "$BASE/$file" "$tmp/$file" || die "download failed: $BASE/$file"
fetch "$BASE/SHA256SUMS" "$tmp/SHA256SUMS" || die "download failed: $BASE/SHA256SUMS"
ok "Downloaded" "$file ($(wc -c < "$tmp/$file" | awk '{printf "%.1f MB", $1/1048576}'))"

working "Verifying checksum"
( cd "$tmp" && grep " $file\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ) || die "checksum mismatch, nothing was installed"
ok "Verified" "SHA-256 matches the published checksum"

tar -xzf "$tmp/$file" -C "$tmp" --no-same-owner fragma
# FRAGMA_INSTALL_ARGS exists for testing, e.g. "--prefix /tmp/x --no-systemctl"
# shellcheck disable=SC2086
"$tmp/fragma" install ${FRAGMA_INSTALL_ARGS:-}
