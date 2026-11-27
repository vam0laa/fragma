#!/bin/sh
# Install, failed update, update and removal on a real system with systemd and
# nftables. It changes that system, so it runs on GitHub's throw-away runners only.
set -eu
[ "${GITHUB_ACTIONS:-}" = true ] || { echo "only on a GitHub Actions runner" >&2; exit 1; }
cd "$(dirname "$0")/../.."
repo=$PWD
t=$(mktemp -d)
chmod 755 "$t"
bin=/usr/local/bin/fragma
unit=/etc/systemd/system/fragma.service

CGO_ENABLED=0 go build -trimpath -o "$t/good" .
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$t/newer" .
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.buildFault=yes" -o "$t/broken" .

cleanup() {
    code=$?
    if [ $code != 0 ]; then sudo journalctl -u fragma -n 60 --no-pager || :; fi
    if [ -x $bin ]; then sudo $bin uninstall || :; fi
    sudo nft delete table ip fragma 2>/dev/null || :
    rm -rf "$t"
    exit $code
}
trap cleanup EXIT

failures=0
check() {
    what=$1
    shift
    if "$@" > "$t/check" 2>&1; then
        echo "  ok: $what"
    else
        echo "  FAIL: $what"
        sed 's/^/      /' "$t/check"
        failures=$((failures + 1))
    fi
}
not() { if "$@"; then return 1; else return 0; fi; }
rules() { sudo nft list table ip fragma; }
runs() { systemctl is-active --quiet fragma; }
boots() { systemctl is-enabled --quiet fragma; }
web() { curl -fsS --max-time 20 -o /dev/null https://github.com; }
install_with() { # program, expected success (yes or no)
    if sudo "$1" install > "$t/out" 2>&1; then got=yes; else got=no; fi
    sed 's/^/    | /' "$t/out"
    [ $got = "$2" ]
}

echo "first install"
check "installs" install_with "$t/good" yes
check "runs" runs
check "starts at boot" boots
check "rules are set" rules
check "the web still works" web

echo "update to a version that does not start"
sudo cp $unit "$t/unit.before"
check "reports failure" install_with "$t/broken" no
check "says the previous version is back" grep -q "previous version is back" "$t/out"
check "the earlier program is back" cmp $bin "$t/good"
check "it runs" runs
check "starts at boot" boots
check "rules are set" rules
check "the service file is as before" cmp $unit "$t/unit.before"
check "no leftover" test ! -e $bin.rollback
check "the web still works" web

echo "update to a good version"
check "installs" install_with "$t/newer" yes
check "the new program is installed" cmp $bin "$t/newer"
check "the earlier one is kept" cmp $bin.old "$t/good"
check "runs" runs
check "rules are set" rules

echo "a failed update while fragma is switched off"
sudo systemctl disable --now fragma
check "reports failure" install_with "$t/broken" no
check "the earlier program is back" cmp $bin "$t/newer"
check "it stays off" not runs
check "does not start at boot" not boots
check "no rules" not rules
check "the kept version is still the one before" cmp $bin.old "$t/good"

echo "removal, then a first install that does not start"
sudo $bin uninstall
check "nothing is left" test ! -e $bin -a ! -e $bin.old -a ! -e $unit
check "reports failure" install_with "$t/broken" no
check "nothing is left" test ! -e $bin -a ! -e $unit
check "no rules" not rules
check "the web still works" web

echo "install.sh, as a user who may use sudo and as root"
mkdir "$t/release" "$t/pack"
cp "$t/good" "$t/pack/fragma"
tar -C "$t/pack" -czf "$t/release/fragma-linux-amd64.tar.gz" fragma
(cd "$t/release" && sha256sum *.tar.gz > SHA256SUMS)
chmod -R a+rX "$t/release"
check "as a user" env FRAGMA_BASE_URL="file://$t/release" sh "$repo/install.sh"
check "  installed and running" sh -c "cmp $bin $t/good && systemctl is-active --quiet fragma"
sudo $bin uninstall
check "as root" sudo env FRAGMA_BASE_URL="file://$t/release" sh "$repo/install.sh"
check "  installed and running" sh -c "cmp $bin $t/good && systemctl is-active --quiet fragma"

[ "$failures" = 0 ] && echo PASS || { echo "$failures checks failed"; exit 1; }
