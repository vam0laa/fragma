#!/bin/sh
# install.sh against made-up releases in a folder, installing into a folder:
# nothing is downloaded and nothing of this computer is touched.
set -eu
cd "$(dirname "$0")/../.."
repo=$PWD
t=$(mktemp -d)
trap 'rm -rf "$t"' EXIT
CGO_ENABLED=0 go build -o "$t/fragma" .
version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' main.go)

# only the tools the installer may use, and stand-ins for the rest
mkdir "$t/path"
for c in sh env curl tar gzip sha256sum mktemp rm cp chmod grep awk wc cat tail head; do ln -s "$(command -v $c)" "$t/path/$c"; done
stub() { # name, body
    printf '#!/bin/sh\n%s\n' "$2" > "$t/path/$1"
    chmod +x "$t/path/$1"
}
stub id 'echo 1000'
stub uname 'if [ "$1" = -s ]; then echo "${OS_NAME:-Linux}"; else echo "${CPU:-x86_64}"; fi'
stub nft 'exit 0'
stub systemctl 'exit 0'
stub sudo 'echo "$*" >> "$SUDO_LOG"; exec "$@"'

release() { # folder, program
    rm -rf "$t/$1" && mkdir -p "$t/$1/pack"
    cp "$2" "$t/$1/pack/fragma"
    for a in amd64 arm64; do tar -C "$t/$1/pack" -czf "$t/$1/fragma-linux-$a.tar.gz" fragma; done
    (cd "$t/$1" && sha256sum *.tar.gz > SHA256SUMS)
}
release good "$t/fragma"

failures=0
installed=$t/root/usr/local/bin/fragma
fresh() { # an installation that is already there
    rm -rf "$t/root" "$t/sudo.log" && mkdir -p "$t/root/usr/local/bin"
    echo "the version from before" > "$installed"
}
echo "the version from before" > "$t/before"
untouched() { cmp -s "$installed" "$t/before" && [ ! -e "$t/sudo.log" ]; }
run() { # release folder, then extra variables
    from=$1
    shift
    env -i PATH="$t/path" HOME="$t" TMPDIR="$t" FRAGMA_LANG=en SUDO_LOG="$t/sudo.log" \
        FRAGMA_BASE_URL="file://$t/$from" FRAGMA_INSTALL_ARGS="--prefix $t/root --no-systemctl" "$@" \
        sh "$repo/install.sh" > "$t/out" 2>&1
}
ok() { echo "  ok: $1"; }
bad() { echo "  FAIL: $1"; sed 's/^/      /' "$t/out"; failures=$((failures + 1)); }
installs() { # what, expected text, run arguments
    what=$1 text=$2
    shift 2
    fresh
    if run "$@" && cmp -s "$installed" "$t/fragma" && grep -qF -- "$text" "$t/out"; then ok "$what"; else bad "$what"; fi
}
refuses() { # what, expected text, run arguments
    what=$1 text=$2
    shift 2
    fresh
    if run "$@"; then bad "$what (it reported success)"
    elif ! untouched; then bad "$what (something was changed, or root was used)"
    elif ! grep -qF -- "$text" "$t/out"; then bad "$what (no message '$text')"
    else ok "$what"; fi
}

echo "a good release"
installs "installs, saying provenance was not verified" "provenance NOT verified: the GitHub CLI (gh) is not installed" good
if grep -q "fragma install" "$t/sudo.log" 2>/dev/null || grep -q 'install "\$@"' "$t/sudo.log"; then ok "root is asked only to install"; else bad "sudo was not used for the install"; fi
[ "$(grep -c '^env ' "$t/sudo.log")" = 1 ] && ok "and only once" || bad "sudo used more than once"
installs "a requested version" "Installed" good FRAGMA_VERSION="v$version"

echo "things that must change nothing"
refuses "provenance required but no gh" "provenance could not be verified" good FRAGMA_REQUIRE_PROVENANCE=1
refuses "another version arrives" "the wrong version arrived" good FRAGMA_VERSION=v99.0.0
refuses "a version that is not one" "not a version" good FRAGMA_VERSION='latest'
refuses "a version with a path in it" "not a version" good FRAGMA_VERSION='v1.2.3/../x'
refuses "another system" "for Linux" good OS_NAME=Darwin
refuses "an unknown processor" "unsupported CPU" good CPU=mips
refuses "nothing to download" "download failed" nowhere
release changed "$t/fragma"; echo x >> "$t/changed/fragma-linux-amd64.tar.gz"
refuses "a changed archive" "checksum mismatch" changed
release nosums "$t/fragma"; rm "$t/nosums/SHA256SUMS"
refuses "no SHA256SUMS" "download failed" nosums
release unlisted "$t/fragma"; sed -i '/amd64/d' "$t/unlisted/SHA256SUMS"
refuses "the file is not listed" "no single line" unlisted
release twice "$t/fragma"; line=$(grep amd64 "$t/twice/SHA256SUMS"); echo "$line" >> "$t/twice/SHA256SUMS"
refuses "the file is listed twice" "no single line" twice
printf 'not a program\n' > "$t/text"; release broken "$t/text"
refuses "a program that does not run" "does not run here" broken

echo "an installer that breaks off"
size=$(wc -c < install.sh)
cut_ok=1
# at the quarters, and at every byte of the last two lines (the very last one is only the newline)
for n in $((size / 4)) $((size / 2)) $((size * 3 / 4)) $(seq $((size - 40)) $((size - 2))); do
    fresh
    head -c "$n" install.sh | env -i PATH="$t/path" HOME="$t" TMPDIR="$t" SUDO_LOG="$t/sudo.log" FRAGMA_BASE_URL="file://$t/good" \
        FRAGMA_INSTALL_ARGS="--prefix $t/root --no-systemctl" sh > "$t/out" 2>&1 || :
    untouched || { cut_ok=0; echo "      changed something when cut at byte $n of $size"; }
done
[ $cut_ok = 1 ] && ok "cut at any of these places: nothing happens" || bad "a cut installer changed something"

echo "provenance with the GitHub CLI"
stub gh 'case "$1 $2" in
    "auth status") [ "${GH_LOGIN:-yes}" = yes ] ;;
    "attestation verify") [ "$3" = --help ] && exit 0; [ "${GH_RESULT:-pass}" = pass ] && exit 0
        case $GH_RESULT in
            net) echo "Error: failed to fetch bundle with URL: Get \"https://store.example/x\": net/http: TLS handshake timeout" ;;
            gone) echo "Error: failed to fetch attestations: HTTP 404: Not Found" ;;
            *) echo "Error: no matching attestation" ;;
        esac
        exit 1 ;;
esac'
installs "verified" "GitHub confirms" good
installs "not logged in" "NOT verified: gh is not logged in" good GH_LOGIN=no
installs "an old release without provenance" "NOT verified: this release has no provenance" good GH_RESULT=fail
release new "$t/fragma"; echo '{}' > "$t/new/manifest.json"
refuses "a workflow release that does not verify" "verification FAILED" new GH_RESULT=fail
refuses "required, old release" "provenance could not be verified" good GH_RESULT=fail FRAGMA_REQUIRE_PROVENANCE=1
installs "required and verified" "GitHub confirms" new FRAGMA_REQUIRE_PROVENANCE=1
installs "the attestation store cannot be reached" "NOT verified: GitHub's proof could not be fetched" new GH_RESULT=net
refuses "required, store not reachable" "provenance could not be verified" new GH_RESULT=net FRAGMA_REQUIRE_PROVENANCE=1
refuses "a workflow release GitHub has no proof for" "verification FAILED" new GH_RESULT=gone

[ "$failures" = 0 ] && echo PASS || { echo "$failures checks failed"; exit 1; }
