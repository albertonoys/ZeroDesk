#!/usr/bin/env bash
# Install the Level example as a per-user ZeroDesk app on a device.
# Usage: ./install.sh <user@device-host>
set -euo pipefail
HOST="${1:?usage: ./install.sh <user@device-host>}"
cd "$(dirname "$0")"
COPYFILE_DISABLE=1 tar --no-xattrs --no-mac-metadata -cf - -C zerodesk . \
	| ssh -o ConnectTimeout=8 "$HOST" '
		set -e
		d=~/.local/share/zerodesk/apps/level
		rm -rf "$d.new" && mkdir -p "$d.new" && tar -xf - -C "$d.new"
		chmod 755 "$d.new/level-web"
		rm -rf "$d" && mv "$d.new" "$d"
		echo "installed in $d"'
