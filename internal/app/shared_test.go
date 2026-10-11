package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/plex"
	"github.com/NotHGM/plex-rpc/internal/presence"
)

// A friend's server: no session list for us, but items can be read.
func sharedServer(t *testing.T, items map[string]string) plex.Server {
	t.Helper()
	pms := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "shared-tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if body, ok := items[r.URL.Path]; ok {
			io.WriteString(w, body)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(pms.Close)
	return plex.Server{Name: "Friend", MachineID: "friend", URI: pms.URL, Token: "shared-tok"}
}

func writeTimeline(t *testing.T, path, state string, at time.Time, pos int64) {
	t.Helper()
	entry := fmt.Sprintf("%s [1] INFO - [Web] [Player] Timeline, {\n  \"ratingKey\": \"62539\",\n  \"state\": %q,\n  \"time\": %d,\n  \"duration\": 1348000\n}\n",
		at.Format("Jan 2, 2006 15:04:05.000"), state, pos)
	if err := os.WriteFile(path, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	// Give the file the entry's own time, mirroring a real log whose mtime
	// advances with each line. (Rewrites here can be the same size, which on
	// some filesystems share an mtime tick and would hide the change.)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func TestFromPlexAppSharedServer(t *testing.T) {
	srv := sharedServer(t, map[string]string{
		"/library/metadata/62539": `{"MediaContainer":{"Metadata":[{"type":"episode","ratingKey":"62539",
		  "guid":"plex://episode/1","Guid":[{"id":"imdb://tt1"}],
		  "title":"Pilot","grandparentTitle":"Show","index":1,"parentIndex":1,"duration":1348000}]}}`,
	})
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Update(func(c *config.Config) { c.AccountID, c.AccountName = 77, "me" })
	cfg := store.Get()

	a := New(store, "test", "", t.TempDir())
	a.servers = []plex.Server{srv}
	logPath := filepath.Join(t.TempDir(), "Plex.log")
	a.appLog.Path = logPath
	f := a.filter(cfg)
	ctx := context.Background()

	writeTimeline(t, logPath, "playing", time.Now().Add(-5*time.Second), 242000)
	c := a.fromPlexApp(ctx, cfg, nil, f)
	if c == nil {
		t.Fatal("expected a candidate")
	}
	if c.Session.Title != "Pilot" || c.Session.GUID != "plex://episode/1" || c.Server.MachineID != "friend" {
		t.Fatalf("wrong item: %+v", c.Session)
	}
	if off := int64(c.Session.ViewOffset); off < 246000 || off > 250000 {
		t.Fatalf("position not extrapolated: %d", off)
	}
	if !f.Match(*c) {
		t.Fatal("candidate should pass the default filter (this PC, own account)")
	}
	if p := presence.Pick([]presence.Candidate{*c}, f); p == nil {
		t.Fatal("candidate not picked")
	}

	// A playing timeline that has gone quiet means playback is over.
	writeTimeline(t, logPath, "playing", time.Now().Add(-time.Minute), 242000)
	if c := a.fromPlexApp(ctx, cfg, nil, f); c != nil {
		t.Fatal("stale timeline should be ignored")
	}

	// Paused stays until the pause timeout hides it.
	writeTimeline(t, logPath, "paused", time.Now().Add(-time.Minute), 242000)
	c = a.fromPlexApp(ctx, cfg, nil, f)
	if c == nil || c.Session.Player.State != "paused" || c.Session.ViewOffset != 242000 {
		t.Fatalf("paused: got %+v", c)
	}

	writeTimeline(t, logPath, "stopped", time.Now(), 242000)
	if c := a.fromPlexApp(ctx, cfg, nil, f); c != nil {
		t.Fatal("stopped should show nothing")
	}
}

func TestFromPlexAppDurationMismatch(t *testing.T) {
	// Rating keys are per server: a different item with the same key on the
	// friend's server must not be shown.
	srv := sharedServer(t, map[string]string{
		"/library/metadata/62539": `{"MediaContainer":{"Metadata":[{"type":"movie","ratingKey":"62539","title":"Other","duration":5400000}]}}`,
	})
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	a := New(store, "test", "", t.TempDir())
	a.servers = []plex.Server{srv}
	a.appLog.Path = filepath.Join(t.TempDir(), "Plex.log")
	writeTimeline(t, a.appLog.Path, "playing", time.Now(), 1000)
	if c := a.fromPlexApp(context.Background(), cfg, nil, a.filter(cfg)); c != nil {
		t.Fatalf("got %+v, want nil", c.Session)
	}
}

func TestFromPlexAppOnlyWithSharedServers(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := store.Get()
	a := New(store, "test", "", t.TempDir())
	a.servers = []plex.Server{{Name: "Home", MachineID: "home", Owned: true}}
	a.appLog.Path = filepath.Join(t.TempDir(), "Plex.log")
	writeTimeline(t, a.appLog.Path, "playing", time.Now(), 1000)
	if c := a.fromPlexApp(context.Background(), cfg, nil, a.filter(cfg)); c != nil {
		t.Fatal("owned servers report playback themselves")
	}
}
