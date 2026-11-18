package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// As a system service (User=pi, the Store layout) the agent starts without
// the variables a login session has. It needs them to talk to the user's
// systemd (launcher, web-app backends) and to PulseAudio.
func ensureSessionEnv() {
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		dir := fmt.Sprintf("/run/user/%d", os.Getuid())
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			os.Setenv("XDG_RUNTIME_DIR", dir)
		}
	}
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		if _, err := os.Stat(filepath.Join(rt, "bus")); err == nil {
			os.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(rt, "bus"))
		}
	}
	if os.Getenv("HOME") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			os.Setenv("HOME", home)
		}
	}
}
