#!/usr/bin/env bash
# Build the UI and the arm64 agent, install on the Zero, keep port 80 working.
# Usage: ./deploy.sh <user@device-host>
set -euo pipefail
HOST="${1:?usage: ./deploy.sh <user@device-host>}"
cd "$(dirname "$0")"

# a device with the package installed runs the system service on the same
# ports; development installs would fight it
if ssh -4 -o ConnectTimeout=8 "$HOST" 'systemctl is-enabled --quiet zerodesk.service 2>/dev/null'; then
	echo "the ZeroDesk package is installed on $HOST; install a new package (packaging/build-deb.sh) instead" >&2
	exit 1
fi

(cd ui && pnpm build)
(cd agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/zerodesk .)
hwapps/stage.sh agent/build/apps

# one compressed stream (about half the size): matters on slow relayed links
COPYFILE_DISABLE=1 tar --no-xattrs --no-mac-metadata -cf - -C agent/build zerodesk -C ../deploy zerodesk.service -C ../../vfb zd_vfb.c -C ../agent/build apps \
	| xz -6 -T0 \
	| ssh -4 -o ConnectTimeout=8 -o ServerAliveInterval=10 -o ServerAliveCountMax=6 "$HOST" \
		'rm -rf /tmp/zd-deploy && mkdir /tmp/zd-deploy && xz -dc | tar -xf - -C /tmp/zd-deploy &&
		 mv /tmp/zd-deploy/zerodesk /tmp/zerodesk.new && mv /tmp/zd-deploy/zerodesk.service /tmp/zerodesk.service &&
		 mv /tmp/zd-deploy/zd_vfb.c /tmp/zd_vfb.c &&
		 rm -rf /tmp/zd-apps && mv /tmp/zd-deploy/apps /tmp/zd-apps && rmdir /tmp/zd-deploy'
ssh -4 -o ConnectTimeout=8 -o ServerAliveInterval=10 -o ServerAliveCountMax=6 "$HOST" '
set -e
mkdir -p ~/.local/bin ~/.config/systemd/user
install -m 755 /tmp/zerodesk.new ~/.local/bin/zerodesk
install -m 644 /tmp/zerodesk.service ~/.config/systemd/user/zerodesk.service
rm -f /tmp/zerodesk.new /tmp/zerodesk.service
# bundled hardware apps (hwapps/): the dev install'"'"'s share dir is the per-user apps dir
mkdir -p ~/.local/share/zerodesk/apps
for d in /tmp/zd-apps/*/; do id=$(basename "$d"); rm -rf ~/.local/share/zerodesk/apps/"$id"; cp -R "$d" ~/.local/share/zerodesk/apps/"$id"; done
rm -rf /tmp/zd-apps
# virtual-screen library for apps opened on the computer
mkdir -p ~/.local/lib/zerodesk
gcc -O2 -shared -fPIC -o ~/.local/lib/zerodesk/zd_vfb.so /tmp/zd_vfb.c -ldl
rm -f /tmp/zd_vfb.c
# port 80: nothing to do when the device lets ordinary programs bind it
# (net.ipv4.ip_unprivileged_port_start <= 80, see docs/DEVELOPMENT.md);
# otherwise replacing the file dropped its capability, so grant it again
if [ "$(cat /proc/sys/net/ipv4/ip_unprivileged_port_start)" -gt 80 ]; then
	sudo -n /usr/sbin/setcap cap_net_bind_service=+ep ~/.local/bin/zerodesk \
		|| echo "warning: port 80 stays off until: sudo setcap cap_net_bind_service=+ep ~/.local/bin/zerodesk"
fi
systemctl --user daemon-reload
systemctl --user enable zerodesk.service >/dev/null 2>&1
systemctl --user restart zerodesk.service
sleep 1
systemctl --user is-active zerodesk.service
journalctl --user -u zerodesk -n 2 --no-pager -o cat
'
