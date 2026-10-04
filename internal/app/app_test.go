//go:build !windows

package app

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NotHGM/plex-rpc/internal/config"
)

const sessionsJSON = `{"MediaContainer":{"Metadata":[{
  "type":"movie","sessionKey":"1","ratingKey":"42","title":"Heat","year":1995,
  "duration":10200000,"viewOffset":60000,"Genre":[{"tag":"Crime"}],
  "User":{"id":"1","title":"owner"},
  "Player":{"address":"203.0.113.9","product":"Plex for Windows","state":"playing","title":"%HOST%"}}]}}`

// End to end: fake Plex server and fake Discord socket, one real tick.
func TestTickSendsActivity(t *testing.T) {
	host, _ := os.Hostname()
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/identity":
			io.WriteString(w, `{"MediaContainer":{"machineIdentifier":"m1"}}`)
		case "/":
			io.WriteString(w, `{"MediaContainer":{"friendlyName":"Test Server"}}`)
		case "/status/sessions":
			io.WriteString(w, replaceHost(sessionsJSON, host))
		case "/library/metadata/42":
			io.WriteString(w, `{"MediaContainer":{"Metadata":[{"ratingKey":"42","Guid":[{"id":"imdb://tt0113277"},{"id":"tmdb://949"}]}]}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer pms.Close()

	activities := fakeDiscord(t)

	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.SetToken("tok")
	_ = store.Update(func(c *config.Config) {
		c.ServerURL = pms.URL
		c.DiscordClientID = "999"
		c.AccountName = "owner"
	})

	a := New(store, "test", "", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a.tick(ctx)
	first := <-activities
	if first["details"] != "Heat" || first["type"] != float64(3) {
		t.Fatalf("activity %v", first)
	}
	if st := a.Status(); st.State != StatePlaying || st.Server != "Test Server" {
		t.Errorf("status %+v", st)
	}

	// Buttons arrive once the background metadata lookup finishes.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-a.wake:
		case <-time.After(200 * time.Millisecond):
		}
		a.sentAt = time.Time{} // skip the rate limit in the test
		a.tick(ctx)
		select {
		case act := <-activities:
			if b, ok := act["buttons"].([]any); ok && len(b) == 2 {
				return
			}
		default:
		}
	}
	t.Fatal("buttons never sent")
}

func replaceHost(s, host string) string { return strings.ReplaceAll(s, "%HOST%", host) }

func fakeDiscord(t *testing.T) chan map[string]any {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("TMPDIR", "")
	ln, err := net.Listen("unix", filepath.Join(dir, "discord-ipc-0"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan map[string]any, 16)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		read := func() (uint32, map[string]any, error) {
			var hdr [8]byte
			if _, err := io.ReadFull(conn, hdr[:]); err != nil {
				return 0, nil, err
			}
			body := make([]byte, binary.LittleEndian.Uint32(hdr[4:]))
			if _, err := io.ReadFull(conn, body); err != nil {
				return 0, nil, err
			}
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			return binary.LittleEndian.Uint32(hdr[:4]), m, nil
		}
		write := func(v any) {
			body, _ := json.Marshal(v)
			hdr := make([]byte, 8)
			binary.LittleEndian.PutUint32(hdr, 1)
			binary.LittleEndian.PutUint32(hdr[4:], uint32(len(body)))
			conn.Write(append(hdr, body...))
		}
		if _, _, err := read(); err != nil {
			return
		}
		write(map[string]any{"evt": "READY", "data": map[string]any{"user": map[string]any{"username": "me"}}})
		for {
			_, cmd, err := read()
			if err != nil {
				return
			}
			if act, ok := cmd["args"].(map[string]any)["activity"].(map[string]any); ok {
				out <- act
			}
			write(map[string]any{"cmd": "SET_ACTIVITY", "nonce": cmd["nonce"]})
		}
	}()
	return out
}
