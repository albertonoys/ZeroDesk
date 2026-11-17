# ZeroDesk development

Developer notes. End-user documentation is in the [README](../README.md).

## Layout

| Path | What |
|---|---|
| `agent/` | Device service (Go, single static arm64 binary). The web UI is embedded, so every front-end change needs a Go rebuild |
| `agent/deploy/zerodesk.service` | systemd **user** unit, listens on `:8686` and `:80` |
| `ui/` | Front end: Svelte 5 (runes) + Vite, a fork of [PuruVJ/macos-web](https://github.com/PuruVJ/macos-web) (MIT, see `ui/LICENSE`). Path alias `🍎/` = `ui/src/`. Build output goes to `agent/web/` |
| `packaging/` | Debian package (nfpm): `build-deb.sh`, the package's systemd user unit, maintainer scripts |
| `examples/level/` | Example app with its own ZeroDesk window (docs/APP-SPEC.md) |
| `vfb/` | `LD_PRELOAD` virtual framebuffer for running third-party device apps inside a window |
| `design/icon-gen/` | Generated icon artwork; `ui/scripts/build-icons.py` turns it into `ui/public/icons/*.webp` |
| `docs/macos-reference.md` | Measured macOS 26/27 specs (window, sidebar, toolbar, lists). Follow it |
| `docs/cardputer-settings.md` | Every device setting and the command or file behind it |
| `docs/APP-SPEC.md` | Format for apps that ship a native ZeroDesk window |

## Windows and their data sources

| Window | Source |
|---|---|
| Cardputer | `/dev/fb0` (320×170 RGB565), only changed rows are sent; keys are written to the `tca8418` evdev node |
| Linux desktop | system wayvnc started on demand (127.0.0.1 only, stopped 20 s after the last viewer), shown with noVNC |
| Finder | `/api/fs/*`, uploads stream to disk, `.deb` installs via `sudo dpkg -i` |
| Terminal | pty + xterm.js |
| Apps | `/usr/share/APPLaunch/applications/*.desktop`; opening one stops the launcher service, runs the app and restores the launcher when it exits |
| Settings | `/api/settings`, `/api/wifi/*`, `/api/system/reboot` (see `docs/cardputer-settings.md`) |
| Voice Memos | `/api/rec/*`, `/api/audio/peaks` |
| Web apps | `/api/webapps/*` and `/x/<capability>/` (docs/APP-SPEC.md); backends as transient `zerodesk-app-<id>` user units |
| Piano | Splendid Grand Piano samples via `/api/piano/{name}` (fetched once by the device, cached in `~/.local/share/zerodesk/piano`) |

## Build

```bash
cd ui && pnpm install && pnpm build
cd ../agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/zerodesk .
```

## Package

```bash
packaging/build-deb.sh 0.1.0      # -> dist/zerodesk_0.1.0_arm64.deb
```

Needs pnpm, Go and Docker (the virtual-screen library is compiled in an arm64 `debian:trixie` container). The package installs `/usr/bin/zerodesk`, `/usr/lib/zerodesk/zd_vfb.so` and the user unit `/usr/lib/systemd/user/zerodesk.service`; `postinst` grants `cap_net_bind_service` (port 80), enables the unit globally and starts it for logged-in users. Tested install, start, port 80 and removal on a device (2026-11-18).

## Deploy to a device

```bash
./deploy.sh pi@<device-host>
```

Builds both parts, copies the binary and the unit, compiles the virtual-framebuffer library on the device, re-grants `cap_net_bind_service` (replacing the file drops it) and restarts the service. Run it with a timeout; SSH over a flaky Wi-Fi link can hang.

A development install lives in `~/.local/bin/zerodesk` with its unit in `~/.config/systemd/user/`; that unit takes precedence over the package's, so remove it before testing a package install.

Web UI auth: cookie `zd_token` = contents of `~/.config/zerodesk/token` on the device. Never print or commit the token.

## Screenshot checks

Before calling a UI change done, screenshot it in light and dark at 1440 and 375 px wide: Playwright with the local Chrome (`channel: 'chrome'`), `deviceScaleFactor: 2`, cookie `zd_token`, and `localStorage['zerodesk:preferences'] = {"appearance":"light"|"dark","theme":{"scheme":"light"|"dark"},"lang":"zh"}` via `addInitScript`. Screenshot `section.window.active` for app windows. Dock items have `aria-label`. Check for line overflow and visible scrollbars.

## UI rules

1. Research the real macOS app / HIG first; match real layouts (`docs/macos-reference.md`).
2. Reuse existing libraries instead of hand-rolling (in use: wavesurfer.js, framework7-icons, smplr).
3. Official CardputerZero apps get native macOS-style web apps, not an emulated screen.
4. Icons: original art in macOS 27 grammar, light and dark variants, flat, real tile corner shape; third-party icons without a white plate.
5. No blue accent, no gradients in UI chrome (icon art is exempt). Short labels, no explanatory prose.

## Known gotchas

- lightningcss drops `backdrop-filter` when a `-webkit-` version follows it: write only the unprefixed property.
- The default ALSA device has no capture; the recorder uses `plughw:N,M` from `/proc/asound/pcm`.
- `~/.config/cardputerzero/config.json` belongs to the launcher; the agent only replaces top-level keys (`brightness`, `dark_time`, `volume`) and the launcher may overwrite them.
- Piano sample names mix case (`PP`, `Mp`, `Mf`, `MF`, `FF`); the agent's name check must allow lower case.

## Measured footprint (2026-11-17, real device)

| State | ZeroDesk | wayvnc |
|---|---|---|
| Idle | 5.9 MB | not running |
| Device screen + Linux desktop open | 8.7 MB / 7% CPU | 39 MB / 10% CPU |

## Open work

1. Native apps for the official CardputerZero apps (Compass, Camera, IR Remote / IR Chat, LoRa, LoRa GPS, NFC, SubG Chat, Keyboard Guide, Factory Test), built in the docs/APP-SPEC.md format where it fits. Hardware notes per app: Compass reads BMI270/BMM150 from IIO sysfs (find the device by `name`, the index differs between units); IR apps use `/dev/lirc*` (mode2 receive, pulse send, 38 kHz); GPS is NMEA on `/dev/ttyS0` at 115200; LoRa (SX1262), NFC (ST25R3916) and SubG (CC1101) need user-space SPI drivers and share SPI0 and GPIO 22/23/26, and the Cap 5 V rail must never be switched off while the LCD is in use; Camera needs libcamera (`rpicam-*`).
2. Voice Memos: keep the playhead centred from the start (wavesurfer's autoCenter only centres after it scrolls).
3. The virtual-screen window: clarify with the user what still needs to change before touching it.
