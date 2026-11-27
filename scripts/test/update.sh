#!/bin/sh
# What an update does to a Linux system, with the real program but a stand-in for
# systemd, in a throw-away folder: nothing of this computer is touched.
set -eu
cd "$(dirname "$0")/../.."
t=$(mktemp -d)
trap 'rm -rf "$t"' EXIT
CGO_ENABLED=0 go build -o "$t/fragma" .

# three "versions" that differ only in their last bytes
for v in old new broken; do cp "$t/fragma" "$t/$v"; echo "$v" >> "$t/$v"; done

# The stand-in: a service starts unless the installed program is the broken one.
mkdir "$t/bin"
cat > "$t/bin/systemctl" <<'FAKE'
#!/bin/sh
s=$FAKE/state
echo "$*" >> "$FAKE/calls"
case "$1" in
    daemon-reload|reset-failed) ;;
    enable) echo yes > "$s.enabled" ;;
    disable) rm -f "$s.enabled"; case "$*" in *--now*) rm -f "$s.active" ;; esac ;;
    is-enabled) [ -f "$s.enabled" ] ;;
    start|restart)
        if cmp -s "$FAKE/root/usr/local/bin/fragma" "$FAKE/../broken"; then rm -f "$s.active"; else cp "$FAKE/root/usr/local/bin/fragma" "$s.active"; fi ;;
    stop) rm -f "$s.active" ;;
    is-active) [ -f "$s.active" ] ;;
    show) if [ -f "$s.active" ]; then echo 4242; else echo 0; fi ;;
    *) echo "unexpected: $*" >&2; exit 1 ;;
esac
FAKE
chmod +x "$t/bin/systemctl"

failures=0
check() { # description, command
    what=$1
    shift
    if "$@"; then echo "  ok: $what"; else echo "  FAIL: $what"; failures=$((failures + 1)); fi
}
fresh() { rm -rf "$t/sys" && mkdir -p "$t/sys/root"; export FAKE="$t/sys"; }
install_version() { PATH="$t/bin:$PATH" "$t/$1" install --prefix "$FAKE/root" > "$t/out" 2>&1; }
installed_is() { cmp -s "$FAKE/root/usr/local/bin/fragma" "$t/$1"; }
running_is() { cmp -s "$FAKE/state.active" "$t/$1"; }
unit=etc/systemd/system/fragma.service
absent() { [ ! -e "$1" ]; }
not() { if "$@"; then return 1; else return 0; fi; }

echo "an update to a version that starts"
fresh
check "first install" install_version old
check "it runs" running_is old
check "update" install_version new
check "the new version is installed and runs" running_is new
check "the old one is kept" cmp -s "$FAKE/root/usr/local/bin/fragma.old" "$t/old"
check "installing the same again keeps that old one" install_version new
check "  (still the old one)" cmp -s "$FAKE/root/usr/local/bin/fragma.old" "$t/old"

echo "an update to a version that does not start"
fresh
install_version old
echo "# local unit text" >> "$FAKE/root/$unit"
cp "$FAKE/root/$unit" "$t/unit.before"
check "the update reports failure" not install_version broken
check "the old program is back" installed_is old
check "and it runs again" running_is old
check "the unit file is as before" cmp -s "$FAKE/root/$unit" "$t/unit.before"
check "still starts at boot" test -f "$FAKE/state.enabled"
check "no leftover files" absent "$FAKE/root/usr/local/bin/fragma.new"

echo "the same while fragma was switched off"
fresh
install_version old
PATH="$t/bin:$PATH" systemctl disable --now fragma
check "the update reports failure" not install_version broken
check "the old program is back" installed_is old
check "it stays switched off" absent "$FAKE/state.active"
check "and does not start at boot" absent "$FAKE/state.enabled"

echo "a first install that does not start"
fresh
check "the install reports failure" not install_version broken
check "no program is left" absent "$FAKE/root/usr/local/bin/fragma"
check "no service file is left" absent "$FAKE/root/$unit"
check "nothing starts at boot" absent "$FAKE/state.enabled"

echo "removing"
fresh
install_version old
install_version new
PATH="$t/bin:$PATH" "$t/new" uninstall --prefix "$FAKE/root" > "$t/out" 2>&1
check "uninstall removes the program and the kept one" absent "$FAKE/root/usr/local/bin/fragma"
check "  (the kept one)" absent "$FAKE/root/usr/local/bin/fragma.old"

[ "$failures" = 0 ] && echo PASS || { echo "$failures checks failed"; exit 1; }
