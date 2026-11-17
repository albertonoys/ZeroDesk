#!/usr/bin/env bash
# Build the UI and the arm64 agent, install on the Zero, keep port 80 working.
# Usage: ./deploy.sh <user@device-host>
set -euo pipefail
HOST="${1:?usage: ./deploy.sh <user@device-host>}"
cd "$(dirname "$0")"

(cd ui && pnpm build)
(cd agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/zerodesk .)

# one compressed stream (about half the size): matters on slow relayed links
COPYFILE_DISABLE=1 tar --no-xattrs --no-mac-metadata -cf - -C agent/build zerodesk -C ../deploy zerodesk.service -C ../../vfb zd_vfb.c \
	| xz -6 -T0 \
	| ssh -4 -o ConnectTimeout=8 -o ServerAliveInterval=10 -o ServerAliveCountMax=6 "$HOST" \
		'rm -rf /tmp/zd-deploy && mkdir /tmp/zd-deploy && xz -dc | tar -xf - -C /tmp/zd-deploy &&
		 mv /tmp/zd-deploy/zerodesk /tmp/zerodesk.new && mv /tmp/zd-deploy/zerodesk.service /tmp/zerodesk.service &&
		 mv /tmp/zd-deploy/zd_vfb.c /tmp/zd_vfb.c && rmdir /tmp/zd-deploy'
ssh -4 -o ConnectTimeout=8 -o ServerAliveInterval=10 -o ServerAliveCountMax=6 "$HOST" '
set -e
mkdir -p ~/.local/bin ~/.config/systemd/user
install -m 755 /tmp/zerodesk.new ~/.local/bin/zerodesk
install -m 644 /tmp/zerodesk.service ~/.config/systemd/user/zerodesk.service
rm -f /tmp/zerodesk.new /tmp/zerodesk.service
# virtual-screen library for apps opened on the computer
mkdir -p ~/.local/lib/zerodesk
gcc -O2 -shared -fPIC -o ~/.local/lib/zerodesk/zd_vfb.so /tmp/zd_vfb.c -ldl
rm -f /tmp/zd_vfb.c
# replacing the file drops file capabilities, so grant the port-80 bind again
sudo -n /usr/sbin/setcap cap_net_bind_service=+ep ~/.local/bin/zerodesk
systemctl --user daemon-reload
systemctl --user enable zerodesk.service >/dev/null 2>&1
systemctl --user restart zerodesk.service
sleep 1
systemctl --user is-active zerodesk.service
journalctl --user -u zerodesk -n 2 --no-pager -o cat
'
