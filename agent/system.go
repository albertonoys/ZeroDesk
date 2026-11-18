package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type sysInfo struct {
	Hostname   string            `json:"hostname"`
	OS         string            `json:"os"`
	Kernel     string            `json:"kernel"`
	UptimeSec  int64             `json:"uptimeSec"`
	Load       [3]float64        `json:"load"`
	MemTotalMB int64             `json:"memTotalMB"`
	MemAvailMB int64             `json:"memAvailMB"`
	DiskTotal  uint64            `json:"diskTotal"`
	DiskFree   uint64            `json:"diskFree"`
	TempC      float64           `json:"tempC"`
	Battery    map[string]string `json:"battery,omitempty"`
	Addrs      []ifaceAddr       `json:"addrs"`
	Time       time.Time         `json:"time"`
}

type ifaceAddr struct {
	Iface string `json:"iface"`
	Addr  string `json:"addr"`
}

func readTrim(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func handleSys(w http.ResponseWriter, r *http.Request) {
	var s sysInfo
	s.Hostname, _ = os.Hostname()
	s.Time = time.Now()
	s.Kernel = readTrim("/proc/sys/kernel/osrelease")
	if f, err := os.Open("/etc/os-release"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(sc.Text(), "PRETTY_NAME="); ok {
				s.OS = strings.Trim(v, `"`)
			}
		}
		f.Close()
	}
	if up := strings.Fields(readTrim("/proc/uptime")); len(up) > 0 {
		f, _ := strconv.ParseFloat(up[0], 64)
		s.UptimeSec = int64(f)
	}
	if l := strings.Fields(readTrim("/proc/loadavg")); len(l) >= 3 {
		for i := 0; i < 3; i++ {
			s.Load[i], _ = strconv.ParseFloat(l[i], 64)
		}
	}
	if f, err := os.Open("/proc/meminfo"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			if len(fs) < 2 {
				continue
			}
			kb, _ := strconv.ParseInt(fs[1], 10, 64)
			switch fs[0] {
			case "MemTotal:":
				s.MemTotalMB = kb / 1024
			case "MemAvailable:":
				s.MemAvailMB = kb / 1024
			}
		}
		f.Close()
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		s.DiskTotal = st.Blocks * uint64(st.Bsize)
		s.DiskFree = st.Bavail * uint64(st.Bsize)
	}
	if t, err := strconv.ParseFloat(readTrim("/sys/class/thermal/thermal_zone0/temp"), 64); err == nil {
		s.TempC = t / 1000
	}
	if ents, _ := os.ReadDir("/sys/class/power_supply"); len(ents) > 0 {
		base := "/sys/class/power_supply/" + ents[0].Name() + "/"
		s.Battery = map[string]string{}
		for _, k := range []string{"capacity", "status", "voltage_now", "current_now", "time_to_empty_now"} {
			if v := readTrim(base + k); v != "" {
				s.Battery[k] = v
			}
		}
	}
	if ifs, err := net.Interfaces(); err == nil {
		for _, i := range ifs {
			if i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := i.Addrs()
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
					s.Addrs = append(s.Addrs, ifaceAddr{i.Name, ipn.IP.String()})
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, s)
}

// handleDebInstall runs `dpkg -i <path>` as administrator (privileged.go)
// and streams its output.
func handleDebInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path     string `json:"path"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	p, err := cleanPath(body.Path)
	if err != nil || !strings.HasSuffix(p, ".deb") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path must be an absolute .deb file"})
		return
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	err = withAdmin(ctx, body.Password, func() error {
		streamDpkgInstall(ctx, w, p)
		return nil
	})
	writeAdminError(w, err, nil)
}

func streamDpkgInstall(ctx context.Context, w http.ResponseWriter, p string) {
	cmd := exec.CommandContext(ctx, "sudo", "-n", "dpkg", "-i", p)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if err := cmd.Start(); err != nil {
		io.WriteString(w, "error: "+err.Error()+"\n")
		return
	}
	go func() {
		err := cmd.Wait()
		if err != nil {
			io.WriteString(pw, "\n[exit] "+err.Error()+"\n")
		} else {
			io.WriteString(pw, "\n[exit] 0\n")
		}
		pw.Close()
	}()
	buf := make([]byte, 4096)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}
