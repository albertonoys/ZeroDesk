#!/usr/bin/env bash
# Build the UI and the arm64 agent, install on the Zero, keep port 80 working.
# Usage: ./deploy.sh [host]   (default: pi@<device-ip>)
set -euo pipefail
HOST="${1:-pi@<device-ip>}"
cd "$(dirname "$0")"

(cd ui && pnpm build)
(cd agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/zerodesk .)

scp -q agent/build/zerodesk "$HOST":/tmp/zerodesk.new
scp -q agent/deploy/zerodesk.service "$HOST":/tmp/zerodesk.service
scp -q vfb/zd_vfb.c "$HOST":/tmp/zd_vfb.c
ssh -4 -o ConnectTimeout=8 "$HOST" '
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
