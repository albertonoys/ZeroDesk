package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// The full Linux desktop (labwc on the HDMI / headless output) is reached
// through the stock wayvnc. ZeroDesk starts wayvnc only while a browser
// window shows the desktop, bound to 127.0.0.1 so it is never exposed on
// the LAN, and bridges it to the browser over the authenticated WebSocket.
// The last viewer leaving stops it again, so it costs no RAM when unused.

const (
	vncAddr       = "127.0.0.1:5960"
	vncIdleStop   = 20 * time.Second
	vncMaxFPS     = "15"
	headlessWidth = 1280
	headlessHigh  = 720
)

type desktopVNC struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	viewers int
	timer   *time.Timer
}

func newDesktopVNC() *desktopVNC { return &desktopVNC{} }

func waylandEnv() []string {
	env := os.Environ()
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		env = append(env, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", os.Getuid()))
	}
	return env
}

// findDesktop returns the Wayland socket and output of the desktop session:
// the first socket whose compositor reports an enabled output. A connected
// HDMI screen wins over the headless placeholder.
func findDesktop(ctx context.Context) (display, output string, headless bool, err error) {
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	ents, _ := os.ReadDir(runtime)
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "wayland-") || strings.HasSuffix(name, ".lock") {
			continue
		}
		cmd := exec.CommandContext(ctx, "wlr-randr")
		cmd.Env = append(waylandEnv(), "WAYLAND_DISPLAY="+name)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		var cur, best string
		var curEnabled, bestHeadless bool
		flush := func() {
			if cur != "" && curEnabled {
				isHeadless := strings.HasPrefix(cur, "NOOP") || strings.HasPrefix(cur, "HEADLESS")
				if best == "" || (bestHeadless && !isHeadless) {
					best, bestHeadless = cur, isHeadless
				}
			}
		}
		sc := bufio.NewScanner(strings.NewReader(string(out)))
		for sc.Scan() {
			line := sc.Text()
			if line != "" && !strings.HasPrefix(line, " ") {
				flush()
				cur, curEnabled = strings.Fields(line)[0], false
				continue
			}
			if strings.TrimSpace(line) == "Enabled: yes" {
				curEnabled = true
			}
		}
		flush()
		if best != "" {
			return name, best, bestHeadless, nil
		}
	}
	return "", "", false, fmt.Errorf("no running desktop session found in %s", runtime)
}

func (d *desktopVNC) acquire(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.viewers++
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	if d.cmd != nil && d.cmd.ProcessState == nil {
		return nil
	}
	display, output, headless, err := findDesktop(ctx)
	if err != nil {
		d.viewers--
		return err
	}
	env := append(waylandEnv(), "WAYLAND_DISPLAY="+display)
	if headless {
		// 1080p over a 1 GHz pixman renderer is sluggish; 720p is ~2.25x less work.
		mode := exec.CommandContext(ctx, "wlr-randr", "--output", output,
			"--custom-mode", fmt.Sprintf("%dx%d", headlessWidth, headlessHigh))
		mode.Env = env
		if out, err := mode.CombinedOutput(); err != nil {
			log.Printf("vnc: set headless mode: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	host, port, _ := net.SplitHostPort(vncAddr)
	cmd := exec.Command("wayvnc", "--config=/dev/null", "--max-fps="+vncMaxFPS,
		"--output="+output, host, port)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		d.viewers--
		return fmt.Errorf("start wayvnc: %w", err)
	}
	d.cmd = cmd
	go func(c *exec.Cmd) {
		err := c.Wait()
		log.Printf("vnc: wayvnc exited: %v", err)
	}(cmd)
	log.Printf("vnc: wayvnc started for %s/%s (pid %d)", display, output, cmd.Process.Pid)
	// wait until it listens
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", vncAddr, 200*time.Millisecond); err == nil {
			c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

func (d *desktopVNC) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.viewers--
	if d.viewers > 0 || d.cmd == nil {
		return
	}
	d.timer = time.AfterFunc(vncIdleStop, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.viewers > 0 || d.cmd == nil {
			return
		}
		if d.cmd.Process != nil {
			_ = d.cmd.Process.Signal(os.Interrupt)
		}
		d.cmd = nil
		log.Printf("vnc: idle, wayvnc stopped")
	})
}

// handleVNCWS bridges RFB bytes between the browser (noVNC) and wayvnc.
func (s *server) handleVNCWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"binary"}})
	if err != nil {
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := s.vnc.acquire(ctx); err != nil {
		_ = c.Close(websocket.StatusInternalError, truncate(err.Error(), 120))
		return
	}
	defer s.vnc.release()

	tcp, err := net.DialTimeout("tcp", vncAddr, 3*time.Second)
	if err != nil {
		_ = c.Close(websocket.StatusInternalError, "desktop unavailable")
		return
	}
	defer tcp.Close()

	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := tcp.Read(buf)
			if n > 0 {
				if werr := c.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		if _, err := tcp.Write(data); err != nil {
			return
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
