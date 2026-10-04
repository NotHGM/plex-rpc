//go:build windows

package discord

import (
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dial(n int) (net.Conn, error) {
	timeout := 500 * time.Millisecond
	return winio.DialPipe(fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, n), &timeout)
}
