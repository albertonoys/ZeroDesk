package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// The extra listener (-also, :80 by default) that makes
// http://<hostname>.local work without a port. Binding ports below 1024
// needs either CAP_NET_BIND_SERVICE on the binary (the package's postinst
// sets it) or net.ipv4.ip_unprivileged_port_start <= 80. When neither holds,
// Settings offers to allow it once with the administrator password and
// then binds again here, without a restart.

const port80Sysctl = "/etc/sysctl.d/90-zerodesk.conf"

var alsoListen struct {
	mu      sync.Mutex
	addr    string
	handler http.Handler
	up      bool
}

func startAlsoListen(addr string, h http.Handler) {
	alsoListen.mu.Lock()
	alsoListen.addr, alsoListen.handler = addr, h
	alsoListen.mu.Unlock()
	if addr == "" {
		return
	}
	if err := tryAlsoListen(); err != nil {
		log.Printf("also-listen %s: %v", addr, err)
	}
}

// tryAlsoListen binds the extra address unless it is already serving.
func tryAlsoListen() error {
	alsoListen.mu.Lock()
	defer alsoListen.mu.Unlock()
	if alsoListen.up {
		return nil
	}
	if alsoListen.addr == "" {
		return errors.New("extra listener disabled (-also)")
	}
	ln, err := net.Listen("tcp", alsoListen.addr)
	if err != nil {
		return err
	}
	alsoListen.up = true
	srv := &http.Server{Handler: alsoListen.handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		err := srv.Serve(ln)
		log.Printf("also-listen %s: %v", ln.Addr(), err)
		alsoListen.mu.Lock()
		alsoListen.up = false
		alsoListen.mu.Unlock()
	}()
	return nil
}

func alsoListening() bool {
	alsoListen.mu.Lock()
	defer alsoListen.mu.Unlock()
	return alsoListen.up
}

// allowPort80 lets ordinary programs bind port 80 and above from now on
// (and after reboots), then binds the extra listener. Runs under withAdmin.
func allowPort80() error {
	if _, err := run(10*time.Second, "sudo", "-n", "sh", "-c",
		"echo net.ipv4.ip_unprivileged_port_start=80 > "+port80Sysctl+" && sysctl -q -p "+port80Sysctl); err != nil {
		return err
	}
	return tryAlsoListen()
}
