#!/bin/sh
# Proves that the checks above refuse what they should. Rebuilds dist/.
set -eu
cd "$(dirname "$0")/../.."
t=$(mktemp -d)
trap 'rm -rf "$t"' EXIT
check=scripts/release/check.sh
version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' main.go)

refused() { # what, then the command that must fail
    what=$1
    shift
    if "$@" > "$t/out" 2>&1; then
        echo "FAIL: accepted $what" >&2
        cat "$t/out" >&2
        exit 1
    fi
    echo "refused: $what"
}

$check tag "v$version"
refused "another version's tag" $check tag v99.0.0
refused "a tag without v" $check tag "$version"
refused "a pre-release tag" $check tag "v$version-rc1"
refused "an empty tag" $check tag ""

scripts/release/dist.sh > /dev/null
$check dist dist
cp -r dist "$t/good"
scripts/release/dist.sh > /dev/null
cmp dist/SHA256SUMS "$t/good/SHA256SUMS" || { echo "FAIL: two builds differ" >&2; exit 1; }
echo "two builds give the same files"

spoil() { rm -rf "$t/d" && cp -r "$t/good" "$t/d"; }
sums() { (cd "$t/d" && sha256sum fragma-*.tar.gz fragma-*.zip manifest.json > SHA256SUMS); }
spoil; printf x >> "$t/d/fragma-linux-amd64.tar.gz"; refused "a changed archive" $check dist "$t/d"
spoil; rm "$t/d/fragma-windows-amd64.zip"; refused "a missing file" $check dist "$t/d"
spoil; touch "$t/d/extra"; refused "an extra file" $check dist "$t/d"
spoil; sed -i '/manifest/d' "$t/d/SHA256SUMS"; refused "a file left out of SHA256SUMS" $check dist "$t/d"
spoil; jq '.version = "0.0.1"' "$t/good/manifest.json" > "$t/d/manifest.json"; sums; refused "a manifest of another version" $check dist "$t/d"
spoil; cp "$t/d/fragma-linux-arm64.tar.gz" "$t/d/fragma-linux-amd64.tar.gz"; sums; refused "a program for the wrong processor" $check dist "$t/d"
echo PASS
