# Level — example ZeroDesk app

A bubble level that shows how a CardputerZero app ships a native ZeroDesk window ([docs/APP-SPEC.md](../../docs/APP-SPEC.md)):

| File | Role |
|---|---|
| `zerodesk/manifest.webmanifest` | W3C manifest + `zerodesk` member: window size, permissions (`storage`, `notifications`), backend |
| `zerodesk/index.html`, `app.js`, `app.css` | The window's page; loads `_zd/sdk.css` and `_zd/sdk.js` from ZeroDesk |
| `zerodesk/level-web` | Backend (Python standard library): reads the BMI270 accelerometer from IIO sysfs and answers `GET /api/tilt` on `$ZERODESK_SOCKET` |
| `zerodesk/icon-light.png`, `icon-dark.png` | App icon, light and dark |
| `level.desktop` | How a packaged app declares the web UI next to its LCD app (`X-ZeroDesk-Web=`) |

What it uses from the SDK: `zd.ready` (language), `zd.on('lang')`, `zd.window.setTitle`, `zd.storage` (the zero offset), `zd.notify` (once the device has been level for two seconds). Light and dark come from `sdk.css`.

## Try it

```bash
./install.sh pi@<device-host>
```

This copies `zerodesk/` to `~/.local/share/zerodesk/apps/level/` on the device. Open Launchpad in ZeroDesk; Level is listed there.

## Package it

In a `.deb`, install `zerodesk/` to `/usr/share/level/zerodesk/`, the backend to `/usr/lib/level/level-web` (and point `backend.exec` there), and `level.desktop` to `/usr/share/APPLaunch/applications/`.
