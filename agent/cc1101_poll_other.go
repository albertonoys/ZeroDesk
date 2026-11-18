//go:build !linux

package main

import "time"

// waitEdge: GPIO character devices exist only on Linux; elsewhere the
// backends never get a line, so this just sleeps.
func (g *gpioLine) waitEdge(d time.Duration) bool {
	time.Sleep(d)
	return false
}
