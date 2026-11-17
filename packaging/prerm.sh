#!/bin/sh
set -e
if [ "$1" = "remove" ]; then
	for u in $(loginctl list-users --no-legend 2>/dev/null | awk '$1 >= 1000 { print $2 }'); do
		systemctl --user -M "$u@" stop zerodesk.service >/dev/null 2>&1 || true
		systemctl --user -M "$u@" stop 'zerodesk-app-*.service' >/dev/null 2>&1 || true
	done
	systemctl --global disable zerodesk.service >/dev/null 2>&1 || true
fi
exit 0
