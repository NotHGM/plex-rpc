//go:build !windows

package discord

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// socketDirs mirrors where Discord (including Flatpak/Snap builds) creates
// its IPC socket.
func socketDirs() []string {
	var base []string
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if v := os.Getenv(env); v != "" {
			base = append(base, v)
		}
	}
	base = append(base, "/tmp")
	var dirs []string
	for _, b := range base {
		dirs = append(dirs, b,
			filepath.Join(b, "app", "com.discordapp.Discord"),
			filepath.Join(b, "snap.discord"))
	}
	return dirs
}

func dial(n int) (net.Conn, error) {
	name := fmt.Sprintf("discord-ipc-%d", n)
	var lastErr error
	for _, d := range socketDirs() {
		conn, err := net.DialTimeout("unix", filepath.Join(d, name), 500*time.Millisecond)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
