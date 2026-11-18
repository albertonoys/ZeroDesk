#!/bin/sh
set -e
# 0.1.0 ran as a per-user service; hand over to the system service.
if [ "$1" = "upgrade" ] && [ -f /usr/lib/systemd/user/zerodesk.service ]; then
	systemctl --global disable zerodesk.service >/dev/null 2>&1 || true
	for u in $(loginctl list-users --no-legend 2>/dev/null | awk '$1 >= 1000 { print $2 }'); do
		systemctl --user -M "$u@" stop zerodesk.service >/dev/null 2>&1 || true
	done
fi
exit 0
