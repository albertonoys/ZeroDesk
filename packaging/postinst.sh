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
		# Restart a few seconds after dpkg finishes, from a unit outside the
		# service's cgroup: ZeroDesk installs .deb files itself (Finder), so
		# the running service may be the very process running this install,
		# and restarting it here would kill dpkg. A development install
		# (deploy.sh: ~/.config/systemd/user/zerodesk.service) holds the same
		# ports and is switched off at the same time.
		stop=""
		for f in /home/*/.config/systemd/user/zerodesk.service; do
			[ -f "$f" ] && stop="$stop systemctl --user -M $(stat -c %U "$f")@ disable --now zerodesk.service;"
		done
		if command -v systemd-run >/dev/null 2>&1; then
			systemd-run --quiet --collect --on-active=5 --unit=zerodesk-restart-$$ \
				sh -c "$stop systemctl reset-failed zerodesk.service; systemctl restart zerodesk.service" || true
		else
			systemctl restart zerodesk.service || true
		fi
	fi
fi
exit 0
