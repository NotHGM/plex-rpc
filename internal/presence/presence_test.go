package presence

import (
	"os"
	"strings"
	"testing"
	"time"

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
		Accounts: []Account{{ID: 4242, Names: []string{"nothgm", "NotHGM"}, Owner: true}},
		ThisPC:   true,
		LocalIPs: map[string]bool{"192.168.1.50": true},
		Hostname: "GEORGE-PC",
		Movies:   true, Episodes: true, Music: true,
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

	f.AllDevices = true
	if !f.Match(c[2]) {
		t.Error("all-devices mode should include the phone")
	}
	f.Music = false
	if f.Match(c[2]) {
		t.Error("music disabled should exclude tracks")
	}

	// Not the owner: only id or name match.
	shared := c[0]
	shared.Server.Owned = false
	f = thisPC()
	f.Accounts[0].Names = nil
	if f.Match(shared) {
		t.Error("local id 1 should only mean 'me' on an owned server")
	}
}

func TestFilterDevicesAndAccounts(t *testing.T) {
	c := loadCandidates(t)
	phone := c[2] // owner, Plexamp on an iPhone
	phone.Session.Player.MachineIdentifier = "iphone-1"
	friend := c[1] // user 8812345 on a Shield
	friend.Session.Player.MachineIdentifier = "shield-1"

	f := thisPC()
	if f.Match(phone) {
		t.Fatal("phone not selected yet")
	}
	f.Devices = map[string]bool{"iphone-1": true, "shield-1": true}
	if !f.Match(phone) {
		t.Error("selected device should match")
	}
	if f.Match(friend) {
		t.Error("selected device but account not shared")
	}
	f.Accounts = append(f.Accounts, Account{ID: 8812345})
	if !f.Match(friend) {
		t.Error("shared Home user on a selected device should match")
	}

	// This PC unticked: only the selected devices.
	f.ThisPC = false
	if f.Match(c[0]) {
		t.Error("this PC should be excluded")
	}

	// No accounts: nothing.
	f.Accounts = nil
	f.AllDevices = true
	if f.Match(c[0]) || f.Match(phone) {
		t.Error("no shared accounts should match nothing")
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
	a := Build(s, Extras{Poster: "https://metadata-static.plex.tv/x.jpg"}, 1_000_000, false)
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

func TestBuildPausedHasNoProgressBar(t *testing.T) {
	// A movie the player still reports as "playing" but which the caller has
	// determined is paused (position not advancing): no timestamps, pause badge.
	s := loadCandidates(t)[1].Session
	s.Player.State = "playing"
	a := Build(s, Extras{}, 1_000, true)
	if a.Timestamps != nil {
		t.Errorf("paused activity must have no timestamps: %+v", a.Timestamps)
	}
	if a.Assets.SmallImage != AssetPause || a.Assets.SmallText != "Paused on Plex for Android (TV)" {
		t.Errorf("small %+v", a.Assets)
	}
}

func TestBuildMovieAndTrack(t *testing.T) {
	c := loadCandidates(t)
	m := Build(c[1].Session, Extras{}, 5, false)
	if m.Details != "Dune: Part Two" || m.State != "2024 · Science Fiction, Adventure · 2h 46m" {
		t.Errorf("movie %q / %q", m.Details, m.State)
	}
	if m.Assets.LargeImage != AssetLogo || m.Assets.LargeText != "Dune: Part Two (2024)" {
		t.Errorf("movie assets %+v", m.Assets)
	}

	tr := Build(c[2].Session, Extras{}, 5, true)
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
	a := Build(s, Extras{}, 100_000, false)
	if Changed(a, Build(s, Extras{}, 105_000, false)) {
		t.Error("5s jitter should not trigger an update")
	}
	if !Changed(a, Build(s, Extras{}, 200_000, false)) {
		t.Error("a seek should trigger an update")
	}
	if !Changed(a, Build(s, Extras{Poster: "https://x/y.jpg"}, 100_000, false)) {
		t.Error("new artwork should trigger an update")
	}
	if !Changed(a, nil) || !Changed(nil, a) || Changed(nil, nil) {
		t.Error("nil handling")
	}
}

func TestTracker(t *testing.T) {
	var tr Tracker
	t0 := time.UnixMilli(1_000_000)
	start, since := tr.Observe("k", 60_000, t0)
	if start != 940_000 || since != 0 {
		t.Fatalf("start %d since %v", start, since)
	}
	// Position unchanged across polls: the anchor holds and sinceMove grows.
	if s, since := tr.Observe("k", 60_000, t0.Add(4*time.Second)); s != start || since != 4*time.Second {
		t.Errorf("anchor %d since %v", s, since)
	}
	// Position advances: sinceMove resets and the anchor stays put (same rate).
	if s, since := tr.Observe("k", 69_000, t0.Add(9*time.Second)); s != start || since != 0 {
		t.Errorf("advancing: anchor %d since %v", s, since)
	}
	// Frozen position (a pause, however the player reports its state).
	if _, since := tr.Observe("k", 69_000, t0.Add(9*time.Second+time.Minute)); since != time.Minute {
		t.Errorf("frozen since %v", since)
	}
	// New item starts fresh.
	if s, since := tr.Observe("other", 0, t0); s != 1_000_000 || since != 0 {
		t.Errorf("new item start %d since %v", s, since)
	}
}

func TestLiveness(t *testing.T) {
	var l Liveness
	t0 := time.Unix(1000, 0)
	if l.Stale("a", "playing", 1000, t0) {
		t.Fatal("new session is not stale")
	}
	// Position advancing: never stale.
	if l.Stale("a", "playing", 9000, t0.Add(25*time.Second)) || l.Stale("a", "playing", 20000, t0.Add(50*time.Second)) {
		t.Fatal("advancing session marked stale")
	}
	// Frozen position while "playing": stale after StaleAfter.
	if l.Stale("a", "playing", 20000, t0.Add(70*time.Second)) {
		t.Fatal("stale too early")
	}
	if !l.Stale("a", "playing", 20000, t0.Add(50*time.Second+StaleAfter)) {
		t.Fatal("frozen session should be stale")
	}
	// Paused sessions are handled by the pause timeout, not here.
	if l.Stale("b", "paused", 5, t0) || l.Stale("b", "paused", 5, t0.Add(time.Hour)) {
		t.Fatal("paused session marked stale")
	}
	// Resuming counts as activity.
	if l.Stale("b", "playing", 5, t0.Add(time.Hour+time.Second)) {
		t.Fatal("resumed session marked stale")
	}
	l.Prune(t0.Add(30 * time.Minute))
	if _, ok := l.seen["a"]; ok {
		t.Fatal("old session not pruned")
	}
}
