#!/bin/sh
# Cross-builds folio for the release targets into dist/:
#
#   scripts/build.sh [VERSION]
#
# Produces dist/folio-<os>-<arch> for linux/amd64, linux/arm64, darwin/amd64
# and darwin/arm64 (static, CGO_ENABLED=0, -trimpath, stripped) plus
# dist/checksums.txt in `sha256sum` format. VERSION defaults to
# `git describe --tags --always --dirty`, else "dev". Set DIST to build
# somewhere else, TARGETS to build a subset ("linux/amd64 darwin/arm64").
set -eu

cd "$(dirname "$0")/.."

version=${1:-$(git describe --tags --always --dirty 2>/dev/null || true)}
[ -n "$version" ] || version=dev
dist=${DIST:-dist}
targets=${TARGETS:-"linux/amd64 linux/arm64 darwin/amd64 darwin/arm64"}

mkdir -p "$dist"
rm -f "$dist"/folio-* "$dist"/checksums.txt

for target in $targets; do
	os=${target%/*}
	arch=${target#*/}
	out="$dist/folio-$os-$arch"
	printf 'building %s (%s)\n' "$out" "$version"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -buildvcs=false \
		-ldflags "-s -w -X main.version=$version" -o "$out" ./cmd/folio
done

cd "$dist"
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum folio-* >checksums.txt
else
	shasum -a 256 folio-* >checksums.txt
fi
printf '\n'
cat checksums.txt
