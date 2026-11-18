#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
	if ! id pi >/dev/null 2>&1; then
		echo "zerodesk: the service runs as user 'pi', which does not exist on this system" >&2
	fi
	# serve http://cardputerzero.local on port 80 without running as root
	if command -v setcap >/dev/null 2>&1; then
		setcap cap_net_bind_service=+ep /usr/bin/zerodesk || true
	fi
	if [ -d /run/systemd/system ]; then
		systemctl daemon-reload || true
		systemctl enable zerodesk.service >/dev/null 2>&1 || true
		systemctl restart zerodesk.service || true
	fi
fi
exit 0
