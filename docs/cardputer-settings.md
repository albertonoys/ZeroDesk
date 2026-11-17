# CardputerZero Settings → commands (for the native Settings app)

Read from CardputerZero/launcher (projects/APPLaunch/main/ui/settings, ext_components/cp0_lvgl/src/cp0) and checked read-only on the device, 2026-11-17.
Launcher runs as `pi`; saved settings live in `~/.config/cardputerzero/config.json` (flat keys, rewritten whole via temp file + rename). Privileged actions use `sudo` (pi has passwordless sudo on this device).

| Section | Setting | Values | Read | Write | Root |
|---|---|---|---|---|---|
| Screen | Brightness | 10–100 % | `/sys/class/backlight/backlight/brightness`, `max_brightness`; config `brightness` | write raw value to `brightness`, save config `brightness` | no (root:video 664) |
| Screen | Screen timeout | Never/10/30/60/300 s | config `dark_time` (default 30) | config `dark_time` | no |
| Speaker | Volume | 0–100 % | PulseAudio default sink; config `volume` | `pactl set-sink-volume @DEFAULT_SINK@ N%`, config `volume` | no |
| Wi-Fi | Enable | on/off | `nmcli radio wifi` | `nmcli radio wifi on\|off` | no |
| Wi-Fi | Networks | list | `nmcli dev wifi rescan`; `nmcli -t -f SSID,SIGNAL,SECURITY,IN-USE dev wifi list`; `nmcli -t --escape no -f UUID,TYPE,NAME con show` | `nmcli --wait 20 dev wifi connect <ssid> password <pw>` (+`hidden yes`); `nmcli con delete uuid <u>`; `nmcli dev disconnect <wlan>` | no |
| Ethernet | Enable / info | on/off | `nmcli -g GENERAL.STATE device show eth0` (100 = up); `/sys/class/net/eth0/address` | `nmcli device connect\|disconnect eth0` | no |
| Bluetooth | Power, Discoverable, Alias | | BlueZ D-Bus `org.bluez.Adapter1` | set the same properties; page entry runs `rfkill unblock bluetooth` | no |
| Bluetooth | Named only | bool | config `bt_named_only` | config | no |
| Bluetooth | Scan / pair | | `StartDiscovery`, `Device1` objects | `Device1.Pair/Connect/Disconnect`, `Adapter1.RemoveDevice` | no |
| ExtPort | Ext 5V / Grove 5V | on/off | `/sys/class/leds/ext_5v_out/brightness`, `/sys/class/leds/grove_5v_out/brightness` | write `1`/`0` | no (root:gpio 664) |
| Battery | Info | read-only | `/sys/class/power_supply/bq27220-0/*` | – | – |
| Launcher | Show/hide built-in apps | bool | config `app_Python`, `app_SSH`, `app_IP_Panel`, `app_Math`, `app_Game`, `app_Tank` | config | no |
| Developer | ADB | on/off | `systemctl is-active/is-enabled adbd.service` | `sudo /usr/share/APPLaunch/adb/cardputer-adb set 1\|0` (also edits /boot/firmware/config.txt) | yes |
| Date & Time | Network time | on/off | `timedate1` `NTP` | `sudo timedatectl set-ntp true\|false` | yes |
| Date & Time | Set manually | YYYY-MM-DD HH:MM:SS | `timedate1` `TimeUSec` | `sudo sh -c "date -s '<ts>'; hwclock -w"` (only when NTP off) | yes |
| System | Software update | | `/var/lib/applaunch-updater/status` | `systemctl start applaunch-apt-update.service` then `applaunch-updater.service` | polkit |
| System | Storage | read-only | `/proc/self/mounts`, `/sys/class/block/*` | – | – |
| System | Reboot | | – | `systemctl reboot -i` | polkit |

Unresolved: BlueZ properties could not be read live (a root `bruce_cardputerzero --ble-spam` process may hold the adapter); `applaunch-apps-updater.service` is not installed on this device.
