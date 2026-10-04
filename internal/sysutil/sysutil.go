// Package sysutil wraps the small OS integrations plex-rpc needs.
package sysutil

import (
	"net"
	"os"
	"strings"
)

// LocalIPs returns every address assigned to this machine.
func LocalIPs() map[string]bool {
	ips := map[string]bool{"127.0.0.1": true, "::1": true}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			ips[n.IP.String()] = true
		}
	}
	return ips
}

// Hostname returns the computer name without a domain suffix.
func Hostname() string {
	h, _ := os.Hostname()
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	return h
}
