# ZeroDesk handoff — 2026-11-18 (previous session)

Read this whole file before doing anything. Reply to the user in Chinese, keep technical terms in English. All git text (commit messages, branch names) in English, no Claude attribution lines.

## What ZeroDesk is

A macOS 27 (Liquid Glass) style web desktop for the M5Stack **CardputerZero** (Raspberry Pi CM based handheld).

- `agent/` — Go device service, systemd **user** service `zerodesk.service` on `:8686` and `:80`. The web UI is embedded in the binary, so **every front-end change needs a Go rebuild** (`deploy.sh` does both).
- `ui/` — Svelte 5 (runes) + Vite fork of PuruVJ/macos-web. Path alias `🍎/` = `ui/src/`.
- `vfb/` — LD_PRELOAD virtual framebuffer for running third-party device apps in a window.
- `design/icon-gen/` — generated icon artwork (codex image generation); `ui/scripts/build-icons.py` turns it into `ui/public/icons/*.webp`.
- `docs/macos-reference.md` — measured macOS 26/27 specs (window 26pt radius, titlebar 52, traffic lights 14pt, sidebar inset 8 radius 18, rows 32, Voice Memos rows 56, glass capsules 36). Follow it.
- `docs/cardputer-settings.md` — every device setting and the command/file behind it.

## Device access

- Zero: user `pi`, passwordless sudo. LAN `<device-ip>`. Tailscale hostname **`cardputerzero`** (installed 2026-11-18; use `tailscale status` to get its 100.x IP). Your SSH key is already in `~pi/.ssh/authorized_keys`.
- Deploy: `./deploy.sh pi@cardputerzero` (default host in the script is the LAN IP; pass the tailnet host). Run with a timeout; SSH can hang.
- Web UI auth: cookie `zd_token` = contents of `~pi/.config/zerodesk/token` on the device. Never print the token; read it into a file and pass it to the screenshot script.
- Zero's Wi-Fi drops for a few seconds now and then (power save) — retry before concluding it is down.

## Rules the user set (non-negotiable)

1. Research the real macOS app / HIG before building anything; match real layouts (see docs).
2. Reuse existing GitHub projects / npm libraries instead of hand-rolling (already adopted: wavesurfer.js 8.1, framework7-icons 5.0.5, smplr 1.2.0).
3. Device apps that are official CardputerZero apps get **native macOS-style web apps** (drag, scroll, click, type naturally), not an emulated screen.
4. Icons: original art in macOS 27 grammar, light + dark variants, flat Apple style (not 3D, not clip-art), real tile corner shape with highlight only on the diagonal corners; third-party app icons without a white plate.
5. **Screenshot and check your own work (light + dark, 1440 and 375 wide) before telling the user it is done.** No line overflow, no visible scrollbars.
6. No blue accent, no gradients in UI chrome (icon art is exempt). Short labels, no explanatory prose in the UI.

## Screenshot method

Playwright via the user's local Chrome (`channel: 'chrome'`), viewport 1440×900, `deviceScaleFactor: 2`, add cookie `zd_token` for `http://<zero>:8686`, set `localStorage['zerodesk:preferences']` to `{"appearance":"light"|"dark","theme":{"scheme":"light"|"dark"},"lang":"zh",...}` via `addInitScript`. Screenshot the element `section.window.active` for app windows. Dock items have `aria-label` (中文 names: 应用 / 访达 / 设置 …).

## Done in this session (all deployed and checked)

- Icons: 15 regenerated (Cardputer full-bleed with safe margin, Raspberry Pi light = red berry + green leaves, NFC = contactless card, 13 official app redesigns). Dock: equal space above/below icons, running dot centred in the bottom space, Trash base level with icon tiles (3.7px padding), Trash darkened in dark mode.
- Launchpad full screen; windows fade out while it is open; opening any app closes it (`open_app` in `ui/src/state/apps.svelte.ts`).
- Windows: default size ≤ 86% of the area between menu bar and Dock; Cardputer window 860×540; resize from all 4 edges + 4 corners (`Window.svelte`, position kept in sync with neodrag via a `position` Compartment).
- Voice Memos (`apps/Recorder`) rebuilt on wavesurfer.js (Timeline + Minimap), full-window recording view with red RESUME; agent `/api/rec/{start,stop,pause,status}`; `/api/audio/peaks` (server-side WAV peaks, cached) so long recordings draw instantly.
- Native **Settings** (`apps/Settings/Settings.svelte`): Wi-Fi (list/join/disconnect), Network, Battery, General (ADB, restart, log out), Appearance, Displays (brightness, screen-off), Sound, Expansion Ports (Ext/Grove 5V), Date & Time. Agent: `agent/settings.go` (`GET/POST /api/settings`, `/api/wifi/networks|connect|disconnect`, `/api/system/reboot`).
- Mic level mapping checked: quiet room = −72 dBFS → 0; no change needed.

## Next tasks, in order

1. **Piano real samples (smplr)** — in progress, nothing written yet.
   - The browser computer can NOT reach `smpldsnds.github.io`; the Zero can (0.8 s per sample).
   - Plan: agent route `GET /api/piano/{name}` that serves `~/.local/share/zerodesk/piano/<name>`, downloading on first request from `https://smpldsnds.github.io/sfzinstruments-splendid-grand-piano/samples/<name>` (validate name with `^[A-Z]{1,3} [A-G]#?-?[0-9]\.(ogg|m4a)$`, one download per file, `.part` + rename, long cache headers).
   - In `ui/src/components/apps/Piano/Piano.svelte`: `SplendidGrandPiano(ctx, { baseUrl: '/api/piano', volume, notesToLoad: { notes: <midi 21–108>, velocityRange: [80, 100] } })`, `await piano.ready`, `piano.start({ note: midi, velocity: 90 })` returns a stop fn; keep the current WebAudio synth as fallback while samples load or if loading fails; sustain pedal keeps notes until released.
   - Check how smplr builds sample URLs (space in names → URL-encoded) in `node_modules/smplr/dist/index.mjs` (`pianoToPreset`, `HttpStorage`).
2. Native apps for the remaining official CardputerZero apps (Compass, Camera, IR Remote/Chat, LoRa, LoRa GPS, NFC, SubG Chat, Keyboard Guide, Factory Test) — research each device app first (sources in the CardputerZero GitHub org), then build macOS-style windows with agent endpoints.
3. Replace remaining hand-drawn SVG glyphs (Music, Recorder, Finder toolbars) with framework7-icons.
4. Voice Memos: playhead should stay centred from the start (wavesurfer autoCenter only centres after it scrolls).
5. Ask the user what "模拟器也没改" means for the virtual-screen window before changing it.

## Known gotchas

- lightningcss drops `backdrop-filter` when a `-webkit-` version follows it: write only the unprefixed property.
- Default ALSA device has no capture; recorder uses `plughw:N,M` found in `/proc/asound/pcm`.
- `~/.config/cardputerzero/config.json` is the launcher's file; agent only replaces top-level keys (`brightness`, `dark_time`, `volume`); the launcher may overwrite them.
- codex image generation: `codex exec --skip-git-repo-check -c 'sandbox_permissions=["disk-full-write-access"]' --image=<ref> "<prompt>" </dev/null` (use `--image=`, close stdin, tell it it may use sips without asking). Jobs can stall 30+ min; kill and restart.
- Work in this repository.
