# ZeroDesk app format (spec version 1)

A CardputerZero ("cp0") app can ship a web UI that ZeroDesk opens as a real window on the computer: native mouse, keyboard and trackpad input, light and dark appearance, resizable, in the Dock and Launchpad. Apps without one still open in the virtual-screen window, as before.

This spec extends how cp0 apps are packaged today instead of adding a parallel system. Each section ends with the decisions taken and why.

## 1. How cp0 apps are packaged today (research, 2026-11-18)

- Apps are arm64 `.deb` packages. The launcher (`CardputerZero/launcher`, APPLaunch) lists `/usr/share/APPLaunch/applications/*.desktop` and reads only `Name`, `Exec`, `Icon`, `Terminal`, `Sysplause`, `Hidden`/`NoDisplay` and `Type`. **Unknown keys are ignored.**
- `Exec` is started directly with `execlp`, without a shell, so apps ship wrapper scripts. The running app owns the LCD and the keyboard until it exits.
- Store packages may install only to `usr/bin/<pkg*>`, `usr/share|usr/lib|opt|var/lib/<pkg*>/`, `etc/<pkg*>`, `usr/share/APPLaunch/**`, `usr/share/doc/**` and `lib/systemd/system/<pkg*>.service` (`CardputerZero/packages`, `install_path_policy.py`). Services must not run as root.
- Store metadata (`meta.json` / the `store` section of `app-builder.json`) carries title, summary, 1–2 of 13 fixed categories, icon (PNG, 128–512 px), 320×170 screenshots and 7 permission booleans (`camera`, `microphone`, `imu`, `network`, `additional_hardware`, `background_service`, `external_display`). Permissions are declarations only.
- There is no web-UI app type. A few store apps run their own web server on a fixed port (for example LoFiBox on `:8765`), with no discovery.

## 2. Discovery

An app opts in with one extra key in the `.desktop` file it already ships:

```ini
[Desktop Entry]
Type=Application
Name=Level
Exec=/usr/lib/level/level-lcd
Icon=/usr/share/level/icon.png
X-ZeroDesk-Web=/usr/share/level/zerodesk
```

`X-ZeroDesk-Web` names a directory that holds `manifest.webmanifest` and the web files. The app id is the `.desktop` file name without `.desktop` (the id ZeroDesk already uses for device apps).

For development, or for apps that only exist on the computer side, ZeroDesk also loads `~/.local/share/zerodesk/apps/<id>/manifest.webmanifest`. If both exist for an id, the system one wins.

Decisions:
- **An `X-` key in the existing `.desktop` file.** The launcher already ignores unknown keys, every app already has this file, and the id stays the same, so ZeroDesk can swap the virtual screen for the web window with no other mapping.
- **A directory, not a URL or port.** Files are served by ZeroDesk itself (no extra server per app, nothing listening on the network) and the directory fits the store's allowed paths (`usr/share/<pkg>/`).
- **A per-user directory** so developers can iterate without building a `.deb`, and so ZeroDesk-only apps need no launcher entry.
- The web directory must resolve (after symlinks) to `/usr/share/`, `/usr/lib/`, `/opt/` or the per-user apps directory; anything else is ignored.

## 3. Manifest

`manifest.webmanifest` is a [W3C Web App Manifest](https://www.w3.org/TR/appmanifest/) with one extra member, `zerodesk`:

```json
{
  "name": "Level",
  "short_name": "Level",
  "description": "Bubble level",
  "start_url": "index.html",
  "categories": ["Hardware & IoT"],
  "icons": [{ "src": "icon-light.png", "sizes": "512x512", "type": "image/png" }],
  "zerodesk": {
    "spec": 1,
    "icons_dark": [{ "src": "icon-dark.png", "sizes": "512x512", "type": "image/png" }],
    "window": { "width": 420, "height": 480, "min_width": 320, "min_height": 360, "resizable": true, "titlebar": "standard" },
    "permissions": ["storage", "notifications"],
    "backend": { "exec": ["/usr/lib/level/level-web"], "memory_max": "32M", "exclusive": true }
  }
}
```

| Member | Meaning |
|---|---|
| `name`, `short_name` | Window title and Dock / Launchpad label (`short_name` preferred when present) |
| `description` | Launchpad tooltip |
| `start_url` | Page loaded in the window, relative to the web directory. Default `index.html` |
| `categories` | One or two of the store's 13 categories |
| `icons` | Light icon; the largest PNG/WebP/SVG is used. Full-bleed square art; ZeroDesk applies the tile mask |
| `zerodesk.spec` | Must be `1` |
| `zerodesk.icons_dark` | Dark-appearance icon. Optional; falls back to `icons` |
| `zerodesk.window` | Initial and minimum size in px, `resizable` (default `true`), `titlebar` (see §5) |
| `zerodesk.permissions` | SDK capabilities the app may use (see §7). Unknown names are ignored |
| `zerodesk.backend` | Optional device-side process (see §4) |

Decisions:
- **Reuse the W3C manifest** rather than a new file format: it is what web developers already write, bundlers and PWA tooling generate it, and its `icons`/`categories`/`start_url` members cover most of what we need. ZeroDesk-specific members live under one namespaced key so the rest stays standard.
- **Dark icons as `zerodesk.icons_dark`.** The W3C manifest has no dark-icon member; ZeroDesk shows light and dark icon variants everywhere, so it needs one.
- **Store categories, not freedesktop categories,** because they are what cp0 users see in the store.
- **Sizes in px with min sizes,** matching ZeroDesk's own window configs.

## 4. Backend (optional)

Apps that need hardware or files ship a small server. ZeroDesk starts it on demand and reverse-proxies to it; the backend never listens on the network.

- `exec`: argv array, run without a shell (same rule as the launcher's `Exec`). The program is an absolute path, or a path relative to the web directory (handy for per-user installs); a backend inside the web directory is never served as a file.
- ZeroDesk starts it with `systemd-run --user` as transient unit `zerodesk-app-<id>.service`, with `MemoryMax=` from `memory_max` (default `48M`), when the first window of the app opens, and stops it 30 s after the last window closes.
- Environment: `ZERODESK_APP_ID`, `ZERODESK_SOCKET` (a Unix socket path under `$XDG_RUNTIME_DIR/zerodesk/apps/`), `ZERODESK_WEB_DIR`.
- The backend must serve HTTP on `ZERODESK_SOCKET` within 10 s. Requests from the window to `api/…` (relative to the page) reach the backend as `/api/…`; WebSocket upgrades are passed through. ZeroDesk strips cookies and adds `X-ZeroDesk-App: <id>`.
- `exclusive: true`: the backend and the app's LCD version use the same hardware, so ZeroDesk refuses to start the backend while that app runs on the device, and the window says so.

Decisions:
- **Unix socket, not a TCP port:** no port collisions between apps, nothing reachable from the LAN, and ZeroDesk's login protects every request.
- **Reverse proxy through ZeroDesk** so apps get authentication and pairing for free and the browser talks to one origin.
- **Transient systemd user units** so each backend has its own cgroup (its memory does not count against ZeroDesk's 64 MB limit), its own journal (`journalctl --user -u zerodesk-app-<id>`), and is cleaned up even if ZeroDesk restarts.
- **Start on demand, stop when idle,** because the device has 512 MB of RAM and most apps are opened briefly.
- **`exclusive`** because cp0 hardware (SPI radios, camera, serial GPS) allows one user at a time; the official apps take exclusive locks.

## 5. The window

The page runs in an `<iframe>` inside a normal ZeroDesk window with traffic lights, resizing, focus and a Dock entry.

`titlebar`:
- `"standard"` (default): ZeroDesk draws the 52 px top band with the traffic lights and the app's name; the page fills the area below.
- `"app"`: the page fills the whole window and draws its own toolbar, like Finder or Music. ZeroDesk overlays the traffic lights at the top-left; the page keeps the top-left corner free (the SDK provides `--zd-inset-top: 52px` and `--zd-inset-left: 84px`), and marks draggable regions with the `data-zd-drag` attribute.

Decisions:
- **iframe in a ZeroDesk window** rather than a separate browser window: it keeps the desktop metaphor, focus, Dock and window management in one place.
- **Two title bar modes** because macOS has both (plain titled windows and unified toolbars), and apps with sidebars need the unified one to look right.

## 6. Isolation

- The iframe is `sandbox="allow-scripts allow-forms allow-modals allow-popups allow-downloads allow-pointer-lock"` **without** `allow-same-origin`, so the page has an opaque origin.
- Its files are served under a per-window capability path `/x/<capability>/`. The capability is a random 128-bit value issued by ZeroDesk when the window opens and revoked when it closes. Relative URLs (scripts, images, `api/…`, WebSockets) carry it automatically.
- Responses under `/x/` send `Access-Control-Allow-Origin: *` (module scripts and `fetch` from an opaque origin need CORS; the path, not a cookie, is the credential) and `Content-Security-Policy: frame-ancestors 'self'`.

Decisions:
- **Opaque origin:** the page cannot read ZeroDesk's DOM, storage or login cookie, and ZeroDesk's `SameSite=Strict` cookie is not sent with its requests, so a buggy or hostile app cannot call ZeroDesk's own APIs (files, Wi-Fi, reboot). Everything it may do goes through the SDK, which checks `permissions`.
- **Capability paths instead of cookies** because an opaque origin cannot hold cookies; scoping the capability to one window and one app limits what a leaked URL can reach.
- Side effect: `localStorage` and `IndexedDB` are not available. The SDK provides `zd.storage` instead.

## 7. JavaScript SDK

Include it from the page (served by ZeroDesk, always matching the running version):

```html
<link rel="stylesheet" href="_zd/sdk.css" />
<script src="_zd/sdk.js"></script>
```

`sdk.css` defines ZeroDesk's colour tokens (`--zd-text`, `--zd-bg`, `--zd-accent`, …) for both appearances, system fonts, and the title bar insets. `sdk.js` defines `window.zd`:

| API | Permission | |
|---|---|---|
| `await zd.ready` | – | `{ app, theme, lang, permissions }` |
| `zd.theme`, `zd.on('theme', fn)` | – | `'light'` or `'dark'`. The SDK also sets `data-theme` and `color-scheme` on `<html>` |
| `zd.lang`, `zd.on('lang', fn)` | – | `'zh'` or `'en'` |
| `zd.window.setTitle(text)` | – | Window title |
| `zd.window.resize(w, h)` | – | Clamped to the screen and the minimum size |
| `zd.window.close()` | – | |
| `zd.storage.get(key)`, `.set(key, value)`, `.remove(key)` | `storage` | JSON values, 1 MiB per app, kept on the device |
| `zd.notify({ title, body })` | `notifications` | Banner at the top-right of the desktop |
| `zd.files.list(path)`, `zd.files.read(path)` | `files.read` | Paths in the device user's home; `read` returns a `Blob` |
| `zd.files.write(path, blob)` | `files.write` | |
| `zd.device.info()` | `device.info` | Model, OS, memory, battery, network |

Calls without the permission reject with `Error('permission: <name>')`. Hardware specific to an app goes through its own backend, not the SDK.

Messages are `postMessage` objects `{ zd: 1, id, method, params }` answered by `{ zd: 1, id, result | error }`; events are `{ zd: 1, event, data }`. ZeroDesk only accepts messages whose `source` is that window's iframe.

Decisions:
- **`postMessage` bridge** because it is the only channel across an opaque-origin iframe, and it lets ZeroDesk check every call against the declared permissions.
- **SDK served by ZeroDesk** instead of an npm package, so apps work with any build tool (or none) and always match the host. A typed npm package can wrap the same protocol later.
- **Small, explicit permission list** mapped to existing ZeroDesk features; device hardware stays in the app's backend, which already runs with the app's own Unix permissions.

## 8. Privileges

- Backends run as the device's normal user (the one ZeroDesk runs as), in their own transient user unit. ZeroDesk never starts a backend as root.
- Hardware access comes from that user's groups (`gpio`, `i2c`, `spi`, `dialout`, `video`, `audio`, `input` on the standard image), as the Store's `meshzero` package does for its radio. A package may add a udev rule (without `RUN`/`PROGRAM`) to give a device node to one of those groups.
- An app that needs root for something must not ask ZeroDesk for it. The CardputerZero Store rejects applications that run with root privileges; if an action truly needs an administrator, the user enters their password for that one action, as ZeroDesk's own Settings and the Store do.

Decision: no `root` permission in the manifest. It would contradict the Store rule, and a backend that needs root cannot be sandboxed by the iframe anyway.

## 9. Packaging checklist

```
usr/share/APPLaunch/applications/level.desktop   # adds X-ZeroDesk-Web=/usr/share/level/zerodesk
usr/share/level/zerodesk/manifest.webmanifest
usr/share/level/zerodesk/index.html, *.js, *.css, icons
usr/lib/level/level-web                          # optional backend
usr/lib/level/level-lcd                          # the LCD app, as before
```

All paths are inside the store's install-path policy. A working example lives in `examples/level/` (bubble level: Python standard-library backend reading the IMU from IIO sysfs, plain HTML/JS front end).

## 10. Versioning

`zerodesk.spec` is `1`. ZeroDesk ignores manifests with a higher number, so an app can declare a newer spec without breaking older ZeroDesk releases. New SDK methods are added without changing the number; removals or behaviour changes bump it.
