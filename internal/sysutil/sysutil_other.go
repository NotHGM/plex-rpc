//go:build !windows

package sysutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// OpenURL opens a link or folder with the default handler.
func OpenURL(target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, target).Start()
}

// AutostartEnabled is only implemented on Windows.
func AutostartEnabled() bool { return false }

// SetAutostart is only implemented on Windows.
func SetAutostart(bool) error { return errors.New("autostart is only supported on Windows") }

// RefreshAutostart is a no-op outside Windows.
func RefreshAutostart() {}

// SingleInstance always succeeds outside Windows.
func SingleInstance() bool { return true }

// Alert prints the message.
func Alert(title, text string) { fmt.Fprintf(os.Stderr, "%s: %s\n", title, text) }
