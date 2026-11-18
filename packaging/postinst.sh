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
		# A development install (deploy.sh: ~/.config/systemd/user/zerodesk.service)
		# holds the same ports and may be the very process running this install
		# (ZeroDesk installs .deb files itself), so hand over a few seconds after
		# dpkg finishes, from a unit outside its cgroup.
		dev=""
		for f in /home/*/.config/systemd/user/zerodesk.service; do
			[ -f "$f" ] && dev="$dev $(stat -c %U "$f")"
		done
		if [ -n "$dev" ] && command -v systemd-run >/dev/null 2>&1; then
			stop=""
			for u in $dev; do
				stop="$stop systemctl --user -M $u@ disable --now zerodesk.service;"
			done
			systemd-run --quiet --collect --on-active=5 --unit=zerodesk-handover \
				sh -c "$stop systemctl reset-failed zerodesk.service; systemctl restart zerodesk.service" || true
		else
			systemctl restart zerodesk.service || true
		fi
	fi
fi
exit 0
