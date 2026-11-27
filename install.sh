#!/bin/sh
# fragma installer: downloads the latest release, verifies its checksum,
# installs it and starts it. Read it before you run it:
#   curl -fsSL https://raw.githubusercontent.com/vam0laa/fragma/main/install.sh | sudo sh
# Run without sudo, it downloads and checks as you and asks for root only to install.
#   FRAGMA_VERSION=v0.5.0         install that version instead of the latest
#   FRAGMA_REQUIRE_PROVENANCE=1   install only if GitHub confirms who built the file
# Language: English, or Turkish when your locale is Turkish (force with FRAGMA_LANG=tr|en).
set -eu

REPO=vam0laa/fragma

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
      os)        echo "bu betik Linux içindir" ;;
      badver)    echo "geçersiz sürüm: $2 (örnek: v0.5.0)" ;;
      nosum)     echo "SHA256SUMS içinde $2 için tek bir satır yok, hiçbir şey kurulmadı" ;;
      badfile)   echo "indirilen program bu sistemde çalışmadı, hiçbir şey kurulmadı" ;;
      wrongver)  echo "istenen sürüm gelmedi ($2), hiçbir şey kurulmadı" ;;
      l_prov)    echo "Kaynak    " ;;
      d_prov)    echo "GitHub doğruladı: bu dosyayı projenin yayın akışı üretti" ;;
      l_noprov)  echo "kaynak DOĞRULANMADI" ;;
      p_nogh)    echo "GitHub CLI (gh) kurulu değil" ;;
      p_oldgh)   echo "bu gh sürümü kaynak doğrulayamıyor" ;;
      p_login)   echo "gh ile giriş yapılmamış (gh auth login)" ;;
      p_none)    echo "bu sürümde kaynak kanıtı yok (0.4.0 ve öncesi)" ;;
      p_net)     echo "GitHub'daki kanıt indirilemedi (ağ hatası)" ;;
      provfail)  echo "kaynak doğrulaması BAŞARISIZ, hiçbir şey kurulmadı: $2" ;;
      provreq)   echo "kaynak doğrulanamadı ve zorunlu tutuldu, hiçbir şey kurulmadı" ;;
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
      os)        echo "this installer is for Linux" ;;
      badver)    echo "not a version: $2 (for example v0.5.0)" ;;
      nosum)     echo "SHA256SUMS has no single line for $2, nothing was installed" ;;
      badfile)   echo "the downloaded program does not run here, nothing was installed" ;;
      wrongver)  echo "the wrong version arrived ($2), nothing was installed" ;;
      l_prov)    echo "Provenance" ;;
      d_prov)    echo "GitHub confirms the project's release workflow built this file" ;;
      l_noprov)  echo "provenance NOT verified" ;;
      p_nogh)    echo "the GitHub CLI (gh) is not installed" ;;
      p_oldgh)   echo "this version of gh cannot verify provenance" ;;
      p_login)   echo "gh is not logged in (gh auth login)" ;;
      p_none)    echo "this release has no provenance (0.4.0 and earlier)" ;;
      p_net)     echo "GitHub's proof could not be fetched (network error)" ;;
      provfail)  echo "provenance verification FAILED, nothing was installed: $2" ;;
      provreq)   echo "provenance could not be verified and is required, nothing was installed" ;;
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

fetch() { # url dest
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else die "$(m curl)"; fi
}

# note: a warning that does not stop the installation
note() { [ -n "$LIVE" ] && printf '\r\033[K'; printf '  %b!%b %s\n' "$R" "$N" "$1"; }

unverified() { # reason
  [ "${FRAGMA_REQUIRE_PROVENANCE:-}" != 1 ] || { note "$(m l_noprov): $1"; die "$(m provreq)"; }
  note "$(m l_noprov): $1"
}

# provenance: who built the file. The checksum only shows that it arrived whole.
provenance() {
  command -v gh >/dev/null 2>&1 || { unverified "$(m p_nogh)"; return 0; }
  gh attestation verify --help >/dev/null 2>&1 || { unverified "$(m p_oldgh)"; return 0; }
  gh auth status >/dev/null 2>&1 || { unverified "$(m p_login)"; return 0; }
  if gh attestation verify "$tmp/$file" --repo "$REPO" \
      --signer-workflow "$REPO/.github/workflows/release.yml" >"$tmp/gh.log" 2>&1; then
    ok "$(m l_prov)" "$(m d_prov)"
  elif grep -qiE 'TLS handshake timeout|i/o timeout|dial tcp|no such host|connection (refused|reset)|network is unreachable|context deadline exceeded|Client\.Timeout' "$tmp/gh.log"; then
    # the proof was not looked at: that is not the same as a proof that is wrong
    unverified "$(m p_net)"
  elif fetch "$base/manifest.json" "$tmp/manifest.json" 2>/dev/null; then
    # releases made by the workflow have a manifest and must verify
    die "$(m provfail "$(grep -v '^$' "$tmp/gh.log" | tail -n 1)")"
  else
    unverified "$(m p_none)"
  fi
}

# install_program: only this needs root. Without it, a root-owned copy is made
# and compared with the file that was checked, so it cannot be swapped in between.
install_program() {
  # FRAGMA_INSTALL_ARGS exists for testing, e.g. "--prefix /tmp/x --no-systemctl"
  if [ "$(id -u)" = 0 ]; then
    # shellcheck disable=SC2086
    "$tmp/fragma" install ${FRAGMA_INSTALL_ARGS:-}
  else
    sum="$(sha256sum "$tmp/fragma" | awk '{print $1}')"
    # shellcheck disable=SC2016,SC2086
    sudo env "FRAGMA_LANG=$L" sh -c 'set -eu
      d="$(mktemp -d)"
      trap "rm -rf \"$d\"" EXIT
      cp "$1" "$d/fragma"
      echo "$2  $d/fragma" | sha256sum -c - >/dev/null
      chmod 755 "$d/fragma"
      shift 2
      "$d/fragma" install "$@"' sh "$tmp/fragma" "$sum" ${FRAGMA_INSTALL_ARGS:-}
  fi
}

# Everything happens in main, called in braces on the last line: a download of
# this script that breaks off anywhere does nothing at all.
main() {
  printf '\n  %b%bfragma%b %b%s%b\n  %b%s%b\n\n' "$B" "$A" "$N" "$B" "$(m title)" "$N" "$D" "$(m tagline)" "$N"

  [ "$(uname -s)" = Linux ] || die "$(m os)"
  [ "$(id -u)" = 0 ] || command -v sudo >/dev/null 2>&1 || die "$(m root)"

  version="${FRAGMA_VERSION:-}"
  if [ -n "$version" ]; then
    printf '%s\n' "$version" | grep -qx 'v[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*' || die "$(m badver "$version")"
    base="https://github.com/$REPO/releases/download/$version"
  else
    base="https://github.com/$REPO/releases/latest/download"
  fi
  base="${FRAGMA_BASE_URL:-$base}" # for testing

  working "$(m w_check)"
  # not every user has the sbin folders in PATH
  command -v nft >/dev/null 2>&1 || [ -x /usr/sbin/nft ] || [ -x /sbin/nft ] || die "$(m nft)"
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

  working "$(m w_dl "$file")"
  fetch "$base/$file" "$tmp/$file" || die "$(m dlfail "$base/$file")"
  fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" || die "$(m dlfail "$base/SHA256SUMS")"
  ok "$(m l_dl)" "$file ($(wc -c < "$tmp/$file" | awk '{printf "%.1f MB", $1/1048576}'))"

  working "$(m w_ver)"
  [ "$(grep -c "  $file\$" "$tmp/SHA256SUMS")" = 1 ] || die "$(m nosum "$file")"
  ( cd "$tmp" && grep "  $file\$" SHA256SUMS | sha256sum -c - >/dev/null 2>&1 ) || die "$(m sumfail)"
  ok "$(m l_ver)" "$(m d_ver)"
  provenance

  tar -xzf "$tmp/$file" -C "$tmp" --no-same-owner fragma || die "$(m badfile)"
  [ -f "$tmp/fragma" ] && [ ! -L "$tmp/fragma" ] || die "$(m badfile)"
  said="$("$tmp/fragma" version 2>/dev/null)" || die "$(m badfile)"
  case "$said" in "fragma "?*) ;; *) die "$(m badfile)" ;; esac
  [ -z "$version" ] || [ "$said" = "fragma ${version#v}" ] || die "$(m wrongver "$said")"

  install_program
}

{ main "$@"; }
