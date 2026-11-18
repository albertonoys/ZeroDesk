package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io/fs"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// The ZeroDesk entry in the CardputerZero launcher (`zerodesk-screen`): a
// small status screen with the address to open on a computer. ZeroDesk
// itself runs as a service; this screen only shows it. Pairing prompts from
// the service are drawn over it; ESC returns to the launcher.
//
// It reads the keyboard without grabbing it, so the service can still grab it
// to read the pairing answer.

type screenInfo struct {
	Running bool
	Host    string   // e.g. cardputerzero.local
	Addrs   []string // LAN IPv4 addresses
	Port80  bool
	Lang    string
}

func pairingFlagPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(dir, "zerodesk-pairing")
}

func gatherScreenInfo() screenInfo {
	info := screenInfo{Lang: launcherLang()}
	host, _ := os.Hostname()
	info.Host = strings.ToLower(host) + ".local"
	for _, port := range []string{"80", "8686"} {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond); err == nil {
			c.Close()
			info.Running = true
			if port == "80" {
				info.Port80 = true
			}
		}
	}
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || strings.HasPrefix(ifc.Name, "tailscale") || strings.HasPrefix(ifc.Name, "docker") {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				info.Addrs = append(info.Addrs, ipn.IP.String())
			}
		}
	}
	return info
}

// launcherLang follows the launcher's language setting when it has one.
func launcherLang() string {
	home, _ := os.UserHomeDir()
	b, err := os.ReadFile(filepath.Join(home, ".config", "cardputerzero", "config.json"))
	if err == nil {
		var cfg map[string]any
		if json.Unmarshal(b, &cfg) == nil {
			for _, k := range []string{"language", "lang", "locale"} {
				if v, ok := cfg[k].(string); ok && strings.HasPrefix(strings.ToLower(v), "zh") {
					return "zh"
				}
			}
		}
	}
	if strings.HasPrefix(os.Getenv("LANG"), "zh") {
		return "zh"
	}
	return "en"
}

type screenText struct {
	running, stopped, open, or, exit string
}

func screenTextFor(lang string) screenText {
	if lang == "zh" {
		return screenText{"运行中", "未运行", "在电脑浏览器中打开", "或", "退出"}
	}
	return screenText{"Running", "Not running", "Open in a browser on your computer", "or", "Exit"}
}

// appIcon decodes ZeroDesk's icon from the embedded web assets.
func appIcon(size int) image.Image {
	b, err := fs.ReadFile(webFS, "web/icons/cardputer-dark.webp")
	if err != nil {
		return nil
	}
	src, err := webp.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	// rounded tile mask
	r := float64(size) * 0.225
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := maxf(maxf(r-float64(x)-0.5, float64(x)+0.5-(float64(size)-r)), 0)
			dy := maxf(maxf(r-float64(y)-0.5, float64(y)+0.5-(float64(size)-r)), 0)
			if dx*dx+dy*dy > r*r {
				dst.SetRGBA(x, y, color.RGBA{})
			}
		}
	}
	return dst
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// renderScreen draws the status screen.
func renderScreen(w, h int, info screenInfo) (*image.RGBA, error) {
	f, err := loadFont(info.Lang)
	if err != nil {
		return nil, err
	}
	t := screenTextFor(info.Lang)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{14, 14, 16, 255}), image.Point{}, draw.Src)
	white := color.RGBA{242, 242, 243, 255}
	dim := color.RGBA{142, 142, 147, 255}
	green := color.RGBA{48, 209, 88, 255}
	red := color.RGBA{255, 69, 58, 255}

	title := face(f, 17)
	small := face(f, 12)
	big := face(f, 20)
	keyFace := face(f, 11)

	// header: icon, name, status
	if ic := appIcon(26); ic != nil {
		draw.Draw(img, image.Rect(14, 10, 40, 36), ic, image.Point{}, draw.Over)
	}
	drawText(img, title, "ZeroDesk", 48, 29, white)
	status, sc := t.stopped, red
	if info.Running {
		status, sc = t.running, green
	}
	sw := textWidth(small, status)
	fillRoundRect(img, image.Rect(w-14-sw-22, 13, w-14, 33), 10, color.RGBA{40, 40, 44, 255})
	fillRoundRect(img, image.Rect(w-14-sw-14, 20, w-14-sw-8, 26), 3, sc)
	drawText(img, small, status, w-14-sw-4, 27, white)

	// address
	port := ""
	if !info.Port80 {
		port = ":8686"
	}
	drawText(img, small, t.open, 16, 62, dim)
	drawText(img, big, "http://"+info.Host+port, 16, 88, white)
	if len(info.Addrs) > 0 {
		alt := t.or + "  http://" + info.Addrs[0] + port
		drawText(img, small, alt, 16, 110, dim)
	}

	// key hint
	kw := textWidth(keyFace, "ESC") + 14
	fillRoundRect(img, image.Rect(16, 138, 16+kw, 158), 5, color.RGBA{58, 58, 62, 255})
	drawCentered(img, keyFace, "ESC", 16+kw/2, 152, white)
	drawText(img, small, t.exit, 16+kw+7, 153, dim)
	return img, nil
}

func toRGB565(img *image.RGBA) []byte {
	b := img.Bounds()
	out := make([]byte, b.Dx()*b.Dy()*2)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			c := img.RGBAAt(x, y)
			v := uint16(c.R>>3)<<11 | uint16(c.G>>2)<<5 | uint16(c.B>>3)
			i := (y*b.Dx() + x) * 2
			out[i] = byte(v)
			out[i+1] = byte(v >> 8)
		}
	}
	return out
}

// runDeviceScreen is the `zerodesk-screen` program started by the launcher.
func runDeviceScreen(fbPath, kbdMatch string) int {
	geo, err := (&screenHub{fbPath: fbPath}).geometry()
	if err != nil {
		fmt.Fprintln(os.Stderr, "zerodesk-screen:", err)
		return 1
	}
	fb, err := os.OpenFile(fbPath, os.O_WRONLY, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "zerodesk-screen:", err)
		return 1
	}
	defer fb.Close()

	quit := make(chan struct{})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; close(quit) }()

	if dev, err := findKeyboardNode(kbdMatch); err == nil {
		if kf, err := os.Open(dev); err == nil {
			defer kf.Close()
			go func() {
				buf := make([]byte, 24*16)
				for {
					n, err := kf.Read(buf)
					if err != nil {
						return
					}
					for i := 0; i+24 <= n; i += 24 {
						typ := binary.LittleEndian.Uint16(buf[i+16:])
						code := binary.LittleEndian.Uint16(buf[i+18:])
						val := int32(binary.LittleEndian.Uint32(buf[i+20:]))
						if typ == evKey && code == 1 && val == 1 { // ESC
							select {
							case <-quit:
							default:
								close(quit)
							}
							return
						}
					}
				}
			}()
		}
	}

	var last []byte
	paint := func() {
		if _, err := os.Stat(pairingFlagPath()); err == nil {
			last = nil // the service's pairing prompt is on screen
			return
		}
		img, err := renderScreen(geo.width, geo.height, gatherScreenInfo())
		if err != nil {
			return
		}
		frame := toRGB565(img)
		if bytes.Equal(frame, last) {
			return
		}
		last = frame
		rb := geo.width * 2
		for y := 0; y < geo.height; y++ {
			_, _ = fb.WriteAt(frame[y*rb:(y+1)*rb], int64(y*geo.stride))
		}
	}
	paint()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-quit:
			return 0
		case <-tick.C:
			paint()
		}
	}
}
