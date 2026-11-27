#!/bin/sh
# Checks release files before anyone gets them.
#   check.sh tag TAG     TAG is vX.Y.Z and names the version in main.go
#   check.sh dist DIR    the files in DIR are complete, whole and agree with each other
# With CLEAN=1 the programs must come from an unchanged checkout.
set -eu
root=$(cd "$(dirname "$0")/../.." && pwd)
fail() { echo "release check failed: $*" >&2; exit 1; }
version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' "$root/main.go")
[ -n "$version" ] || fail "no version in main.go"

case ${1:-} in
tag)
    tag=${2:-}
    echo "$tag" | grep -Eqx 'v[0-9]+\.[0-9]+\.[0-9]+' || fail "tag '$tag' is not of the form vX.Y.Z"
    [ "$tag" = "v$version" ] || fail "tag $tag, but main.go says version $version"
    echo "tag $tag matches the program version"
    ;;
dist)
    cd "${2:?folder}"
    want="SHA256SUMS fragma-linux-amd64.tar.gz fragma-linux-arm64.tar.gz fragma-windows-amd64.zip manifest.json"
    have=$(find . -mindepth 1 | sed 's|^\./||' | LC_ALL=C sort | tr '\n' ' ')
    [ "$have" = "$want " ] || fail "files are: $have"
    listed=$(awk '{print $2}' SHA256SUMS | LC_ALL=C sort | tr '\n' ' ')
    [ "$listed" = "fragma-linux-amd64.tar.gz fragma-linux-arm64.tar.gz fragma-windows-amd64.zip manifest.json " ] ||
        fail "SHA256SUMS lists: $listed"
    sha256sum --strict --quiet -c SHA256SUMS || fail "a checksum does not match"
    [ "$(jq -r .version manifest.json)" = "$version" ] || fail "manifest version"
    commit=$(jq -r .commit manifest.json)
    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT
    for target in linux/amd64 linux/arm64 windows/amd64; do
        os=${target%/*} arch=${target#*/}
        mkdir "$work/$os-$arch"
        if [ "$os" = windows ]; then
            file=fragma-$os-$arch.zip bin=fragma.exe
            if command -v unzip > /dev/null; then
                inside=$(unzip -Z1 "$file")
                unzip -q "$file" -d "$work/$os-$arch"
            else
                inside=$(bsdtar -tf "$file")
                bsdtar -xf "$file" -C "$work/$os-$arch"
            fi
        else
            file=fragma-$os-$arch.tar.gz bin=fragma
            inside=$(tar -tzf "$file")
            tar -xzf "$file" -C "$work/$os-$arch"
        fi
        [ "$inside" = "$bin" ] || fail "$file holds: $inside"
        entry=$(jq -r --arg n "$file" '.files[] | select(.name == $n) | "\(.os)/\(.arch) \(.sha256) \(.size)"' manifest.json)
        [ "$entry" = "$target $(sha256sum "$file" | cut -d' ' -f1) $(wc -c < "$file" | tr -d ' ')" ] || fail "manifest entry of $file: $entry"
        info=$(go version -m "$work/$os-$arch/$bin")
        for line in "GOOS=$os" "GOARCH=$arch" "CGO_ENABLED=0" "vcs.revision=$commit"; do
            echo "$info" | grep -qF "$line" || fail "$file: the program lacks $line"
        done
        if [ "${CLEAN:-}" = 1 ]; then
            echo "$info" | grep -qF "vcs.modified=false" || fail "$file was built from a changed checkout"
        fi
    done
    if [ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" = linux/amd64 ]; then
        said=$("$work/linux-amd64/fragma" version)
        [ "$said" = "fragma $version" ] || fail "the program says '$said'"
    fi
    echo "release files of fragma $version are complete and consistent"
    ;;
*)
    fail "usage: check.sh tag TAG | check.sh dist DIR"
    ;;
esac
