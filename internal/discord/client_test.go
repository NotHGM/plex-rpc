//go:build !windows

package discord

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeDiscord accepts one connection and answers like the Discord client.
func fakeDiscord(t *testing.T, reject string) (got chan map[string]any) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("TMPDIR", "")
	ln, err := net.Listen("unix", filepath.Join(dir, "discord-ipc-0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan map[string]any, 4)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		op, body, err := readFrame(conn)
		if err != nil || op != opHandshake {
			return
		}
		var hs map[string]any
		_ = json.Unmarshal(body, &hs)
		got <- hs
		_ = writeFrame(conn, opFrame, map[string]any{"cmd": "DISPATCH", "evt": "READY",
			"data": map[string]any{"user": map[string]any{"username": "tester"}}})
		for {
			op, body, err := readFrame(conn)
			if err != nil || op != opFrame {
				return
			}
			var cmd map[string]any
			_ = json.Unmarshal(body, &cmd)
			got <- cmd
			// An unrelated event first, to check nonce matching.
			_ = writeFrame(conn, opFrame, map[string]any{"evt": "SOMETHING", "nonce": nil})
			if reject != "" {
				_ = writeFrame(conn, opFrame, map[string]any{"cmd": "SET_ACTIVITY", "evt": "ERROR",
					"nonce": cmd["nonce"], "data": map[string]any{"code": 4000, "message": reject}})
				continue
			}
			_ = writeFrame(conn, opFrame, map[string]any{"cmd": "SET_ACTIVITY", "nonce": cmd["nonce"], "data": nil})
		}
	}()
	return got
}

func TestSetActivity(t *testing.T) {
	got := fakeDiscord(t, "")
	c := New("123")
	err := c.SetActivity(&Activity{Type: TypeWatching, Details: "Breaking Bad", State: "S05E14",
		Timestamps: &Timestamps{Start: 1, End: 2}})
	if err != nil {
		t.Fatal(err)
	}
	hs := <-got
	if hs["client_id"] != "123" || hs["v"] != float64(1) {
		t.Errorf("handshake %v", hs)
	}
	cmd := <-got
	if cmd["cmd"] != "SET_ACTIVITY" {
		t.Errorf("cmd %v", cmd)
	}
	args := cmd["args"].(map[string]any)
	if args["pid"] != float64(os.Getpid()) {
		t.Errorf("pid %v", args["pid"])
	}
	act := args["activity"].(map[string]any)
	if act["type"] != float64(3) || act["details"] != "Breaking Bad" {
		t.Errorf("activity %v", act)
	}
	if c.User() != "tester" {
		t.Errorf("user %q", c.User())
	}

	// Clearing sends no activity key.
	if err := c.SetActivity(nil); err != nil {
		t.Fatal(err)
	}
	cmd = <-got
	if _, ok := cmd["args"].(map[string]any)["activity"]; ok {
		t.Error("clear should omit activity")
	}
}

func TestRPCErrorKeepsConnection(t *testing.T) {
	fakeDiscord(t, "bad activity")
	c := New("123")
	err := c.SetActivity(&Activity{Details: "x"})
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != 4000 {
		t.Fatalf("err = %v", err)
	}
	if !c.Connected() {
		t.Error("an RPC error should not drop the connection")
	}
}

func TestNotRunning(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := os.Stat("/tmp/discord-ipc-0"); err == nil {
		t.Skip("a real Discord client is running")
	}
	if err := New("123").SetActivity(&Activity{}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("err = %v", err)
	}
}
