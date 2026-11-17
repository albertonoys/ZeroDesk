#!/usr/bin/env bash
# Build dist/zerodesk_<version>_arm64.deb.
# Needs: pnpm, Go, Docker (compiles the virtual-screen library for arm64 Debian).
# Usage: packaging/build-deb.sh [version]   (default: from git describe)
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:-$(git describe --tags --always 2>/dev/null | sed 's/^v//; s/-/+/g')}"
case "$VERSION" in [0-9]*) ;; *) VERSION="0.0.0+$VERSION" ;; esac

(cd ui && pnpm install --frozen-lockfile && pnpm build)
(cd agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/zerodesk .)

mkdir -p packaging/build dist
docker run --rm --platform linux/arm64 -v "$PWD/vfb:/src:ro" -v "$PWD/packaging/build:/out" debian:trixie \
	sh -c 'apt-get update -qq && apt-get install -y -qq gcc libc6-dev >/dev/null && gcc -O2 -shared -fPIC -o /out/zd_vfb.so /src/zd_vfb.c -ldl'

cd packaging
ZERODESK_VERSION="$VERSION" go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.0 \
	package --config nfpm.yaml --packager deb --target ../dist/
ls -l ../dist/
