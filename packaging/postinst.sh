#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
	if ! id pi >/dev/null 2>&1; then
		echo "zerodesk-cp0: the service runs as user 'pi', which does not exist on this system" >&2
	fi
	# serve http://cardputerzero.local on port 80 without running as root
	if command -v setcap >/dev/null 2>&1; then
		setcap cap_net_bind_service=+ep /usr/bin/zerodesk-cp0 || true
	fi
	if [ -d /run/systemd/system ]; then
		systemctl daemon-reload || true
		systemctl enable zerodesk-cp0.service >/dev/null 2>&1 || true
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
		# Up to 0.2.8 this package was called zerodesk. That one holds the same
		# ports, so it is switched off and then removed, once dpkg is done. Only
		# ZeroDesk's own: another, unrelated Store app is also named zerodesk.
		old=""
		if dpkg-query -W -f='${db:Status-Status} ${Maintainer}\n' zerodesk 2>/dev/null | grep -q '^installed ZeroDesk'; then
			stop="$stop systemctl disable --now zerodesk.service;"
			old="DEBIAN_FRONTEND=noninteractive apt-get remove -y -o DPkg::Lock::Timeout=120 zerodesk;"
		fi
		# The install may itself run inside the service being stopped, so wait
		# until apt and dpkg have exited (at most a minute) before stopping it.
		wait="for i in \$(seq 60); do pgrep -x dpkg >/dev/null || pgrep -x apt-get >/dev/null || pgrep -x apt >/dev/null || break; sleep 1; done;"
		if command -v systemd-run >/dev/null 2>&1; then
			systemd-run --quiet --collect --on-active=3 --unit=zerodesk-cp0-restart-$$ \
				sh -c "$wait $stop systemctl reset-failed zerodesk-cp0.service; systemctl restart zerodesk-cp0.service; $old" || true
		else
			systemctl restart zerodesk-cp0.service || true
		fi
	fi
fi
exit 0
