#!/bin/sh
# ZeroDesk runs as a systemd user service of the device's desktop user.
set -e
if [ "$1" = "configure" ]; then
	# port 80 without root
	setcap cap_net_bind_service=+ep /usr/bin/zerodesk || true
	systemctl --global enable zerodesk.service >/dev/null 2>&1 || true
	for u in $(loginctl list-users --no-legend 2>/dev/null | awk '$1 >= 1000 { print $2 }'); do
		systemctl --user -M "$u@" daemon-reload >/dev/null 2>&1 || true
		systemctl --user -M "$u@" restart zerodesk.service >/dev/null 2>&1 || true
	done
fi
exit 0
