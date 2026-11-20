#!/bin/sh
set -e
# ZeroDesk 0.1.0 (package zerodesk) ran as a per-user service; the system
# service takes over its ports.
if [ -f /usr/lib/systemd/user/zerodesk.service ]; then
	systemctl --global disable zerodesk.service >/dev/null 2>&1 || true
	for u in $(loginctl list-users --no-legend 2>/dev/null | awk '$1 >= 1000 { print $2 }'); do
		systemctl --user -M "$u@" stop zerodesk.service >/dev/null 2>&1 || true
	done
fi
exit 0
