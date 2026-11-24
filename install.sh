#!/bin/sh
# fragma installer: downloads the latest release, verifies its checksum,
# installs it and starts it. Read it before you run it:
#   curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
# Language: English, or Turkish when your locale is Turkish (force with FRAGMA_LANG=tr|en).
set -eu

BASE="${FRAGMA_BASE_URL:-https://github.com/vam0laa/fragma/releases/latest/download}"

# language: FRAGMA_LANG, then LC_ALL, LC_MESSAGES, LANG (same order as the program)
loc="${FRAGMA_LANG:-}"
[ -n "$loc" ] || loc="${LC_ALL:-}"
[ -n "$loc" ] || loc="${LC_MESSAGES:-}"
[ -n "$loc" ] || loc="${LANG:-}"
case "$loc" in tr*|TR*) L=tr ;; *) L=en ;; esac

# m KEY: a message. Labels are padded to 10 characters by hand, because the
# printf of some shells counts bytes, which breaks the alignment of Turkish letters.
m() {
  if [ "$L" = tr ]; then
    case "$1" in
      title)     echo "yükleyici" ;;
      tagline)   echo "operatörünün engellediği siteleri tek komutla aç" ;;
      root)      echo "yönetici olarak çalıştır: curl -fsSL <url> | sudo sh" ;;
      nft)       echo "nftables gerekli (Arch: pacman -S nftables, Debian/Ubuntu: apt install nftables)" ;;
      systemd)   echo "systemd tabanlı bir sistem gerekli" ;;
      cpu)       echo "desteklenmeyen işlemci: $2" ;;
      curl)      echo "curl veya wget gerekli" ;;
      l_check)   echo "Kontrol   " ;;
      l_dl)      echo "İndirildi " ;;
      l_ver)     echo "Doğrulandı" ;;
      w_dl)      echo "$2 indiriliyor" ;;
      w_ver)     echo "Sağlama toplamı doğrulanıyor" ;;
      d_ver)     echo "SHA-256 yayınlanan sağlama toplamıyla eşleşiyor" ;;
      dlfail)    echo "indirme başarısız: $2" ;;
      sumfail)   echo "sağlama toplamı tutmuyor, hiçbir şey kurulmadı" ;;
      w_check)   echo "Gereksinimler kontrol ediliyor" ;;
    esac
  else
    case "$1" in
      title)     echo "installer" ;;
      tagline)   echo "open the sites your ISP blocks, with one command" ;;
      root)      echo "run as root: curl -fsSL <url> | sudo sh" ;;
      nft)       echo "nftables is required (Arch: pacman -S nftables, Debian/Ubuntu: apt install nftables)" ;;
      systemd)   echo "a systemd based system is required" ;;
      cpu)       echo "unsupported CPU: $2" ;;
      curl)      echo "curl or wget is required" ;;
      l_check)   echo "Checked   " ;;
      l_dl)      echo "Downloaded" ;;
      l_ver)     echo "Verified  " ;;
      w_dl)      echo "Downloading $2" ;;
      w_ver)     echo "Verifying checksum" ;;
      d_ver)     echo "SHA-256 matches the published checksum" ;;
      dlfail)    echo "download failed: $2" ;;
      sumfail)   echo "checksum mismatch, nothing was installed" ;;
      w_check)   echo "Checking requirements" ;;
    esac
  fi
}

# colors only on a terminal, and never when NO_COLOR is set
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  G='\033[38;5;114m'; R='\033[38;5;203m'; D='\033[38;5;244m'; B='\033[1m'; A='\033[38;5;111m'; N='\033[0m'; LIVE=1
else
  G=''; R=''; D=''; B=''; A=''; N=''; LIVE=''
fi

working() { [ -n "$LIVE" ] && printf '  %b…%b %s' "$D" "$N" "$1"; return 0; }
ok()      { [ -n "$LIVE" ] && printf '\r\033[K'; printf '  %b✔%b %b%s%b %b%s%b\n' "$G" "$N" "$B" "$1" "$N" "$D" "$2" "$N"; }
die()     { [ -n "$LIVE" ] && printf '\r\033[K'; printf '  %b✖%b %s\n' "$R" "$N" "$1" >&2; exit 1; }

printf '\n  %b%bfragma%b %b%s%b\n  %b%s%b\n\n' "$B" "$A" "$N" "$B" "$(m title)" "$N" "$D" "$(m tagline)" "$N"

[ "$(id -u)" = 0 ] || die "$(m root)"

working "$(m w_check)"
command -v nft >/dev/null 2>&1 || die "$(m nft)"
command -v systemctl >/dev/null 2>&1 || die "$(m systemd)"
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "$(m cpu "$(uname -m)")" ;;
esac
ok "$(m l_check)" "nftables, systemd, $arch"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
file="fragma-linux-$arch.tar.gz"

fetch() { # url dest
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else die "$(m curl)"; fi
}

working "$(m w_dl "$file")"
fetch "$BASE/$file" "$tmp/$file" || die "$(m dlfail "$BASE/$file")"
fetch "$BASE/SHA256SUMS" "$tmp/SHA256SUMS" || die "$(m dlfail "$BASE/SHA256SUMS")"
ok "$(m l_dl)" "$file ($(wc -c < "$tmp/$file" | awk '{printf "%.1f MB", $1/1048576}'))"

working "$(m w_ver)"
( cd "$tmp" && grep " $file\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ) || die "$(m sumfail)"
ok "$(m l_ver)" "$(m d_ver)"

tar -xzf "$tmp/$file" -C "$tmp" --no-same-owner fragma
# FRAGMA_INSTALL_ARGS exists for testing, e.g. "--prefix /tmp/x --no-systemctl"
# shellcheck disable=SC2086
"$tmp/fragma" install ${FRAGMA_INSTALL_ARGS:-}
