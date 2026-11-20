#!/usr/bin/env bash
# Build both editions of the Debian package:
#   dist/zerodesk-cp0_<version>-<rev>_arm64.deb        CardputerZero Store edition (-tags store)
#   dist/zerodesk-cp0_<version>-<rev>+full_arm64.deb   GitHub edition (everything)
# Needs: pnpm, Go, Docker (compiles the virtual-screen library for arm64 Debian), Python 3 with Pillow.
# Usage: packaging/build-deb.sh <version> [revision=1]
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${1:?usage: packaging/build-deb.sh <version> [revision]}"
REV="${2:-1}"

(cd ui && pnpm install --frozen-lockfile && pnpm build)
mkdir -p agent/build packaging/build dist
for ed in store full; do
	tags=""; [ "$ed" = store ] && tags="-tags store"
	(cd agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $tags -trimpath -ldflags='-s -w' -o build/zerodesk-$ed .)
done

# ZD_VFB_SO=<file>: use a library already compiled on a device (deploy.sh
# builds ~/.local/lib/zerodesk/zd_vfb.so there) instead of Docker
if [ -n "${ZD_VFB_SO:-}" ]; then
	cp "$ZD_VFB_SO" packaging/build/zd_vfb.so
else
	docker run --rm --platform linux/arm64 -v "$PWD/vfb:/src:ro" -v "$PWD/packaging/build:/out" debian:trixie \
		sh -c 'apt-get update -qq && apt-get install -y -qq gcc libc6-dev >/dev/null && gcc -O2 -shared -fPIC -o /out/zd_vfb.so /src/zd_vfb.c -ldl'
fi

# the bundled hardware apps (hwapps/), served from /usr/share/zerodesk-cp0/apps
hwapps/stage.sh packaging/build/apps

# launcher icon (100 px, the launcher's size) and Store icon / screenshots
python3 packaging/store-assets.py

cd packaging
NFPM="go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.0"
cp ../agent/build/zerodesk-store build/zerodesk-cp0
ZERODESK_VERSION="$VERSION" ZERODESK_RELEASE="$REV" \
	$NFPM package --config nfpm.yaml --packager deb --target "../dist/zerodesk-cp0_${VERSION}-${REV}_arm64.deb"
cp ../agent/build/zerodesk-full build/zerodesk-cp0
ZERODESK_VERSION="$VERSION" ZERODESK_RELEASE="${REV}+full" \
	$NFPM package --config nfpm.yaml --packager deb --target "../dist/zerodesk-cp0_${VERSION}-${REV}+full_arm64.deb"
ls -l ../dist/
