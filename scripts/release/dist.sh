#!/bin/sh
# Builds the release files into dist/: the archives, manifest.json and SHA256SUMS.
# The same commit built with the same Go gives the same bytes.
set -eu
cd "$(dirname "$0")/../.."

version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' main.go)
[ -n "$version" ] || { echo "no version in main.go" >&2; exit 1; }
commit=$(git rev-parse HEAD)
epoch=$(git log -1 --format=%ct)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
rm -rf dist
mkdir dist
echo '[]' > "$work/files.json"

for target in linux/amd64 linux/arm64 windows/amd64; do
    os=${target%/*} arch=${target#*/}
    bin=fragma
    [ "$os" = windows ] && bin=fragma.exe
    mkdir "$work/$os-$arch"
    CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" -o "$work/$os-$arch/$bin" .
    chmod 0755 "$work/$os-$arch/$bin"
    TZ=UTC touch -d "@$epoch" "$work/$os-$arch/$bin"
    if [ "$os" = windows ]; then
        file=fragma-$os-$arch.zip
        out=$PWD/dist/$file
        (cd "$work/$os-$arch" && TZ=UTC zip -q -X "$out" "$bin")
    else
        file=fragma-$os-$arch.tar.gz
        tar -C "$work/$os-$arch" --owner=0 --group=0 --numeric-owner --mtime="@$epoch" -cf - "$bin" | gzip -n -9 > "dist/$file"
    fi
    jq --arg name "$file" --arg os "$os" --arg arch "$arch" --arg sha "$(sha256sum "dist/$file" | cut -d' ' -f1)" \
        --argjson size "$(wc -c < "dist/$file")" '. + [{name: $name, os: $os, arch: $arch, sha256: $sha, size: $size}]' \
        "$work/files.json" > "$work/next.json"
    mv "$work/next.json" "$work/files.json"
done

jq -n --arg version "$version" --arg commit "$commit" --arg go "$(go env GOVERSION)" --slurpfile files "$work/files.json" \
    '{name: "fragma", version: $version, commit: $commit, go: $go, files: $files[0]}' > dist/manifest.json
(cd dist && sha256sum fragma-*.tar.gz fragma-*.zip manifest.json > SHA256SUMS)
echo "fragma $version ($commit) in dist/"
