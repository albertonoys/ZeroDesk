# ZeroDesk

CardputerZero 的 macOS 风格网页桌面。窗口外壳 fork 自 [PuruVJ/macos-web](https://github.com/PuruVJ/macos-web)（MIT，许可证在 `ui/LICENSE`）。

## 结构

| 目录 | 内容 |
|---|---|
| `agent/` | 设备端服务（Go，单文件，arm64），网页文件嵌在程序里 |
| `ui/` | 前端（Svelte 5 + Vite），构建产物输出到 `agent/web/` |
| `agent/deploy/zerodesk.service` | systemd 用户服务 |

## 窗口

| 窗口 | 数据来源 |
|---|---|
| Cardputer | 读 `/dev/fb0`（320×170 RGB565），只发生变化的行、不压缩；按键写进 `tca8418c` 的 evdev 节点 |
| Linux 桌面 | 按需启动系统自带的 wayvnc（只监听 127.0.0.1，无人观看 20 秒后停止），noVNC 显示 |
| 访达 | `/api/fs/*`，上传流式落盘，`.deb` 用 `sudo dpkg -i` 安装 |
| 终端 | pty + xterm.js |
| 应用 | `/usr/share/APPLaunch/applications/*.desktop`；打开时停掉启动器服务、运行应用，应用退出后自动恢复启动器 |
| 关于本机 / 设置 | `/api/sys`；中英文、深浅色、减弱动态效果 |

## 构建与部署

```bash
cd ui && pnpm install && pnpm build
```

```bash
cd agent && GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o build/zerodesk .
```

```bash
scp agent/build/zerodesk pi@cardputerzero.local:/tmp/zerodesk.new && ssh pi@cardputerzero.local 'install -m 755 /tmp/zerodesk.new ~/.local/bin/zerodesk && systemctl --user restart zerodesk'
```

前端改动后必须重新编译 Go 程序再部署，网页文件是嵌在程序里的。

## 登录

访问 `http://cardputerzero.local:8686`，口令在设备的 `~/.config/zerodesk/token`。也可以用 `http://cardputerzero.local:8686/#token=<口令>` 一键登录。

## 实测资源占用（2026-11-17，真机）

| 状态 | ZeroDesk | wayvnc |
|---|---|---|
| 空闲 | 5.9 MB | 不运行 |
| 小屏 + Linux 桌面同时打开 | 8.7 MB / 7% CPU | 39 MB / 10% CPU |
