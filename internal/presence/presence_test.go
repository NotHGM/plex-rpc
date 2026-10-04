package presence

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/discord"
	"github.com/NotHGM/plex-rpc/internal/plex"
)

func loadCandidates(t *testing.T) []Candidate {
	t.Helper()
	b, err := os.ReadFile("testdata/sessions.json")
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := plex.ParseSessions(b)
	if err != nil {
		t.Fatal(err)
	}
	srv := plex.Server{Name: "Home", MachineID: "srv1", Owned: true}
	var out []Candidate
	for _, s := range sessions {
		out = append(out, Candidate{Server: srv, Session: s})
	}
	return out
}

func thisPC() Filter {
	return Filter{
		Mode:         config.PlayerThisPC,
		AccountID:    4242,
		AccountNames: []string{"nothgm", "NotHGM"},
		LocalIPs:     map[string]bool{"192.168.1.50": true},
		Hostname:     "GEORGE-PC",
		Movies:       true, Episodes: true, Music: true,
	}
}

func TestParseSessions(t *testing.T) {
	c := loadCandidates(t)
	if len(c) != 5 {
		t.Fatalf("got %d sessions", len(c))
	}
	if c[1].Session.Year != 2024 {
		t.Errorf("year sent as string not decoded: %d", c[1].Session.Year)
	}
	if c[0].Session.ParentIndex != 5 || c[0].Session.Index != 14 {
		t.Errorf("episode numbers: %+v", c[0].Session)
	}
}

func TestFilter(t *testing.T) {
	c := loadCandidates(t)
	f := thisPC()
	want := []bool{
		true,  // owner on this PC (matched by IP)
		false, // another user
		false, // owner, but on a phone
		true,  // owner on this PC behind Docker NAT (matched by hostname)
		false, // photos are not shown
	}
	for i, w := range want {
		if got := f.Match(c[i]); got != w {
			t.Errorf("session %d (%s): match = %v, want %v", i, c[i].Session.Title, got, w)
		}
	}

	f.Mode = config.PlayerAny
	if !f.Match(c[2]) {
		t.Error("any-device mode should include the phone")
	}
	f.Music = false
	if f.Match(c[2]) {
		t.Error("music disabled should exclude tracks")
	}

	// Not the owner: only id or name match.
	shared := c[0]
	shared.Server.Owned = false
	f = thisPC()
	f.AccountNames = nil
	if f.Match(shared) {
		t.Error("local id 1 should only mean 'me' on an owned server")
	}
}

func TestPickPrefersPlaying(t *testing.T) {
	c := loadCandidates(t)
	// Put the paused home video first.
	c[0], c[3] = c[3], c[0]
	got := Pick(c, thisPC())
	if got == nil || got.Session.Title != "Ozymandias" {
		t.Fatalf("picked %+v", got)
	}
	if Pick(c[1:3], thisPC()) != nil {
		t.Fatal("nothing should match")
	}
}

func TestBuildEpisode(t *testing.T) {
	s := loadCandidates(t)[0].Session
	a := Build(s, Extras{Poster: "https://metadata-static.plex.tv/x.jpg"}, 1_000_000)
	if a.Type != discord.TypeWatching || a.StatusDisplayType != discord.DisplayDetails {
		t.Errorf("type %d display %d", a.Type, a.StatusDisplayType)
	}
	if a.Details != "Breaking Bad" || a.State != "S05E14 · Ozymandias" {
		t.Errorf("text %q / %q", a.Details, a.State)
	}
	if a.Assets.LargeImage != "https://metadata-static.plex.tv/x.jpg" || a.Assets.LargeText != "Breaking Bad · Season 5" {
		t.Errorf("assets %+v", a.Assets)
	}
	if a.Assets.SmallImage != AssetPlay || a.Assets.SmallText != "Playing on Plex for Windows" {
		t.Errorf("small %+v", a.Assets)
	}
	if a.Timestamps == nil || a.Timestamps.Start != 1_000_000 || a.Timestamps.End != 1_000_000+2_870_000 {
		t.Errorf("timestamps %+v", a.Timestamps)
	}
}

func TestBuildMovieAndTrack(t *testing.T) {
	c := loadCandidates(t)
	m := Build(c[1].Session, Extras{}, 5)
	if m.Details != "Dune: Part Two" || m.State != "2024 · Science Fiction, Adventure · 2h 46m" {
		t.Errorf("movie %q / %q", m.Details, m.State)
	}
	if m.Assets.LargeImage != AssetLogo || m.Assets.LargeText != "Dune: Part Two (2024)" {
		t.Errorf("movie assets %+v", m.Assets)
	}

	tr := Build(c[2].Session, Extras{}, 5)
	if tr.Type != discord.TypeListening || tr.StatusDisplayType != discord.DisplayState {
		t.Errorf("track type %d", tr.Type)
	}
	if tr.Details != "Get Lucky" || tr.State != "Daft Punk feat. Pharrell Williams" || tr.Assets.LargeText != "Random Access Memories (2013)" {
		t.Errorf("track %+v %+v", tr, tr.Assets)
	}
	if tr.Timestamps != nil || tr.Assets.SmallImage != AssetPause {
		t.Errorf("paused track should have no timestamps and a pause badge: %+v", tr)
	}
}

func TestClamp(t *testing.T) {
	if got := clamp("A"); got != "A⠀" {
		t.Errorf("short: %q", got)
	}
	long := strings.Repeat("é", 200)
	if got := clamp(long); len([]rune(got)) != maxText || !strings.HasSuffix(got, "…") {
		t.Errorf("long: %d runes", len([]rune(got)))
	}
}

func TestChanged(t *testing.T) {
	s := loadCandidates(t)[0].Session
	a := Build(s, Extras{}, 100_000)
	if Changed(a, Build(s, Extras{}, 105_000)) {
		t.Error("5s jitter should not trigger an update")
	}
	if !Changed(a, Build(s, Extras{}, 200_000)) {
		t.Error("a seek should trigger an update")
	}
	if !Changed(a, Build(s, Extras{Poster: "https://x/y.jpg"}, 100_000)) {
		t.Error("new artwork should trigger an update")
	}
	if !Changed(a, nil) || !Changed(nil, a) || Changed(nil, nil) {
		t.Error("nil handling")
	}
}

func TestTracker(t *testing.T) {
	var tr Tracker
	t0 := time.UnixMilli(1_000_000)
	start, _ := tr.Observe("k", "playing", 60_000, t0)
	if start != 940_000 {
		t.Fatalf("start %d", start)
	}
	// Offset not reported again yet: keep the anchor.
	if s, _ := tr.Observe("k", "playing", 60_000, t0.Add(4*time.Second)); s != start {
		t.Errorf("anchor moved to %d", s)
	}
	// Pause and stay paused.
	tr.Observe("k", "paused", 70_000, t0.Add(10*time.Second))
	if _, p := tr.Observe("k", "paused", 70_000, t0.Add(70*time.Second)); p != time.Minute {
		t.Errorf("paused for %v", p)
	}
	// Resume resets the pause timer.
	if _, p := tr.Observe("k", "playing", 70_000, t0.Add(80*time.Second)); p != 0 {
		t.Errorf("paused after resume: %v", p)
	}
	// New item starts fresh.
	if s, _ := tr.Observe("other", "playing", 0, t0); s != 1_000_000 {
		t.Errorf("new item start %d", s)
	}
}
