// Package app runs the polling loop that connects Plex to Discord.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NotHGM/plex-rpc/internal/artwork"
	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/discord"
	"github.com/NotHGM/plex-rpc/internal/plex"
	"github.com/NotHGM/plex-rpc/internal/presence"
	"github.com/NotHGM/plex-rpc/internal/sysutil"
)

// Discord allows 5 activity updates per 20 seconds.
const minUpdateGap = 4 * time.Second

// freezeGrace is how long the reported position may stand still before playback
// is treated as paused. It is comfortably longer than any client's timeline
// reporting interval, so normal playback is never mistaken for a pause.
const freezeGrace = 12 * time.Second

// State is a coarse status for the tray icon.
type State int

const (
	StateStarting State = iota
	StateSignedOut
	StateSigningIn
	StateDisabled
	StateNoServer
	StateDiscordDown
	StateIdle
	StatePlaying
	StatePaused
)

// Status is what the tray shows.
type Status struct {
	State   State
	Text    string
	Server  string
	Account string
}

// App owns all long-lived state.
type App struct {
	store           *config.Store
	plex            *plex.Client
	art             *artwork.Resolver
	defaultClientID string

	wake chan struct{}

	mu             sync.Mutex
	status         Status
	listeners      []func(Status)
	knownListeners []func()
	signIn         context.CancelFunc
	signInGen      int
	reset          bool

	// Loop-owned state (only touched by Run).
	discord    *discord.Client
	servers    []plex.Server
	failures   int
	tracker    presence.Tracker
	sent       *discord.Activity
	synced     bool
	sentAt     time.Time
	lastErr    string
	lastKey    string
	ignored    map[string]bool
	ghosts     map[string]bool
	hosted     bool
	localIPs   map[string]bool
	localIPsAt time.Time

	extrasMu sync.Mutex
	extras   map[string]*presence.Extras // nil value = lookup in progress
}

// New creates the app. defaultClientID is used when the config has none.
func New(store *config.Store, version, defaultClientID string, cacheDir string) *App {
	pc := plex.NewClient(store.Get().ClientIdentifier, version)
	return &App{
		store:           store,
		plex:            pc,
		art:             artwork.New(pc, cacheDir+"/artwork-cache.json"),
		defaultClientID: defaultClientID,
		wake:            make(chan struct{}, 1),
		ignored:         map[string]bool{},
		extras:          map[string]*presence.Extras{},
		synced:          true,
		status:          Status{State: StateStarting, Text: "Starting…"},
	}
}

// SetHosted marks the engine as running inside the Plex process (the mod), so
// it skips the "is Plex running?" ghost check. Call before Run.
func (a *App) SetHosted(v bool) { a.hosted = v }

// OnStatus registers a callback for status changes. It is called from the
// loop goroutine.
func (a *App) OnStatus(fn func(Status)) {
	a.mu.Lock()
	a.listeners = append(a.listeners, fn)
	st := a.status
	a.mu.Unlock()
	fn(st)
}

// Status returns the latest status.
func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

func (a *App) setStatus(st Status) {
	a.mu.Lock()
	if a.signIn != nil && st.State == StateSignedOut {
		st.State, st.Text = StateSigningIn, "Waiting for sign-in in your browser…"
	}
	if st == a.status {
		a.mu.Unlock()
		return
	}
	a.status = st
	ls := append([]func(Status){}, a.listeners...)
	a.mu.Unlock()
	for _, fn := range ls {
		fn(st)
	}
}

// Wake makes the loop run immediately (after a settings change).
func (a *App) Wake() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// Store exposes the settings.
func (a *App) Store() *config.Store { return a.store }

// Run polls until ctx is cancelled, then clears the presence.
func (a *App) Run(ctx context.Context) {
	defer a.shutdown()
	for {
		wait := a.tick(ctx)
		a.art.Save()
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-time.After(wait):
		}
	}
}

func (a *App) shutdown() {
	if a.discord != nil {
		if a.discord.Connected() {
			_ = a.discord.SetActivity(nil)
		}
		a.discord.Close()
	}
	a.art.Save()
}

// tick runs one poll and returns how long to wait before the next one.
func (a *App) tick(ctx context.Context) time.Duration {
	if reloaded, err := a.store.ReloadIfChanged(); err != nil {
		log.Printf("config: %v", err)
	} else if reloaded {
		log.Printf("config reloaded")
		a.servers = nil
	}
	if a.takeReset() {
		a.servers = nil
	}
	cfg := a.store.Get()
	poll := time.Duration(cfg.PollIntervalSeconds) * time.Second
	account := cfg.AccountTitle
	if account == "" {
		account = cfg.AccountName
	}

	token, err := a.store.Token()
	if err != nil {
		log.Printf("config: cannot decrypt Plex token: %v", err)
	}
	if token == "" {
		if a.discord != nil {
			a.push(nil)
		}
		a.setStatus(Status{State: StateSignedOut, Text: "Not signed in to Plex"})
		return poll
	}

	a.ensureDiscord(cfg)
	if a.discord.ClientID() == "" {
		a.setStatus(Status{State: StateDiscordDown, Text: "No Discord application ID (see config.json)", Account: account})
		return poll
	}

	if cfg.Disabled {
		a.push(nil)
		a.setStatus(Status{State: StateDisabled, Text: "Presence paused", Account: account})
		return poll
	}

	if a.servers == nil || a.failures >= 3 {
		servers, err := a.discover(ctx, cfg, token)
		if err != nil {
			if errors.Is(err, plex.ErrUnauthorized) {
				log.Printf("plex: token rejected, signing out")
				a.SignOut()
				return poll
			}
			log.Printf("plex: %v", err)
			a.push(nil)
			a.setStatus(Status{State: StateNoServer, Text: "Can't reach your Plex server", Account: account})
			return 15 * time.Second
		}
		a.servers, a.failures = servers, 0
		for _, s := range servers {
			log.Printf("plex: using server %q at %s", s.Name, s.URI)
		}
		a.refreshAccounts(ctx, token)
	}

	cands, ok := a.poll(ctx)
	if !ok {
		a.failures++
		a.push(nil)
		a.setStatus(Status{State: StateNoServer, Text: "Can't reach your Plex server", Account: account})
		return poll
	}
	a.failures = 0

	filter := a.filter(cfg)
	a.rememberDevices(cands, filter)
	a.logIgnored(cands, filter)
	pick := presence.Pick(presence.DedupeByDevice(a.dropGhosts(cands, filter)), filter)

	serverNames := make([]string, len(a.servers))
	for i, s := range a.servers {
		serverNames[i] = s.Name
	}
	st := Status{Server: strings.Join(serverNames, ", "), Account: account}

	if pick == nil {
		a.tracker.Reset()
		a.lastKey = ""
		a.push(nil)
		st.State, st.Text = StateIdle, "Nothing playing"
		a.setDiscordStatus(&st)
		return poll
	}

	key := presence.Key(*pick)
	if key != a.lastKey {
		s := pick.Session
		log.Printf("now playing: %s %q on %s (%s, %s)", s.Type, title(s), s.Player.Product, s.Player.Title, s.Player.Address)
		a.lastKey = key
	}

	start, sinceMove := a.tracker.Observe(key, int64(pick.Session.ViewOffset), time.Now())
	// A session is paused when the player says so, or when its position has
	// stopped advancing. The latter also catches players that keep reporting
	// their state as "playing" while actually paused, which would otherwise
	// leave the Discord progress bar running ahead of the real position.
	paused := pick.Session.Player.State == "paused" || sinceMove >= freezeGrace
	hide := paused && cfg.ClearAfterPauseMinutes >= 0 &&
		sinceMove >= time.Duration(cfg.ClearAfterPauseMinutes)*time.Minute

	if cfg.Debug {
		matches := 0
		for _, c := range cands {
			if filter.Match(c) {
				matches++
			}
		}
		p := pick.Session.Player
		log.Printf("debug: pick=%q sessionKey=%s type=%s playerState=%s offset=%dms dur=%dms sinceMove=%s paused=%v hide=%v cands=%d matches=%d device=%q@%s",
			title(pick.Session), pick.Session.SessionKey, pick.Session.Type, p.State, int64(pick.Session.ViewOffset), int64(pick.Session.Duration),
			sinceMove.Round(time.Second), paused, hide, len(cands), matches, p.Title, p.Address)
	}

	var next *discord.Activity
	if !hide {
		next = presence.Build(pick.Session, a.extrasFor(ctx, *pick, cfg, token), start, paused)
	}
	a.push(next)

	if paused {
		st.State, st.Text = StatePaused, "Paused: "+title(pick.Session)
	} else {
		verb := "Watching"
		if pick.Session.Type == "track" {
			verb = "Listening to"
		}
		st.State, st.Text = StatePlaying, verb+" "+title(pick.Session)
	}
	a.setDiscordStatus(&st)
	return poll
}

func (a *App) setDiscordStatus(st *Status) {
	if a.discord != nil && !a.synced && !a.discord.Connected() {
		if st.State == StatePlaying || st.State == StatePaused {
			st.Text = "Discord isn't running · " + st.Text
		} else {
			st.Text = "Discord isn't running"
		}
		st.State = StateDiscordDown
	}
	a.setStatus(*st)
}

func title(s plex.Session) string {
	switch s.Type {
	case "episode":
		return fmt.Sprintf("%s – %s", s.GrandparentTitle, s.Title)
	case "track":
		art := s.OriginalTitle
		if art == "" {
			art = s.GrandparentTitle
		}
		return fmt.Sprintf("%s – %s", art, s.Title)
	}
	return s.Title
}

func (a *App) ensureDiscord(cfg config.Config) {
	id := cfg.DiscordClientID
	if id == "" {
		id = a.defaultClientID
	}
	if a.discord != nil && a.discord.ClientID() == id {
		return
	}
	if a.discord != nil {
		a.discord.Close()
	}
	a.discord = discord.New(id)
	a.sent, a.synced = nil, false
}

// push sends next to Discord when it differs from what is shown.
func (a *App) push(next *discord.Activity) {
	if a.synced && !presence.Changed(a.sent, next) {
		return
	}
	if next == nil && !a.discord.Connected() {
		// Nothing is shown when Discord is not connected.
		a.sent, a.synced = nil, true
		return
	}
	if a.synced && time.Since(a.sentAt) < minUpdateGap {
		return // retried on the next poll
	}
	a.sentAt = time.Now()
	if err := a.discord.SetActivity(next); err != nil {
		a.synced = false
		if msg := err.Error(); msg != a.lastErr {
			a.lastErr = msg
			if !errors.Is(err, discord.ErrNotRunning) {
				log.Printf("discord: %v", err)
			} else {
				log.Printf("discord: not running, will keep trying")
			}
		}
		return
	}
	if a.lastErr != "" {
		log.Printf("discord: connected as %s", a.discord.User())
		a.lastErr = ""
	}
	a.sent, a.synced = next, true
}

func (a *App) discover(ctx context.Context, cfg config.Config, token string) ([]plex.Server, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if cfg.ServerURL != "" {
		s, err := a.plex.ServerIdentity(ctx, cfg.ServerURL, token)
		if err != nil {
			return nil, fmt.Errorf("server_url %s: %w", cfg.ServerURL, err)
		}
		return []plex.Server{s}, nil
	}
	return a.plex.OwnedServers(ctx, token)
}

func (a *App) poll(ctx context.Context) ([]presence.Candidate, bool) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		cands []presence.Candidate
		okAny bool
	)
	for _, srv := range a.servers {
		wg.Add(1)
		go func(srv plex.Server) {
			defer wg.Done()
			sessions, err := a.plex.Sessions(ctx, srv)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				log.Printf("plex: %s: %v", srv.Name, err)
				return
			}
			okAny = true
			for _, s := range sessions {
				cands = append(cands, presence.Candidate{Server: srv, Session: s})
			}
		}(srv)
	}
	wg.Wait()
	return cands, okAny
}

func (a *App) filter(cfg config.Config) presence.Filter {
	if a.localIPs == nil || time.Since(a.localIPsAt) > time.Minute {
		a.localIPs, a.localIPsAt = sysutil.LocalIPs(), time.Now()
	}
	devices := map[string]bool{}
	for _, id := range cfg.ShareDevices {
		devices[id] = true
	}
	return presence.Filter{
		Accounts:   accountsFor(cfg, cfg.SharedAccounts()),
		ThisPC:     cfg.ShareThisPC,
		AllDevices: cfg.ShareAllDevices,
		Devices:    devices,
		LocalIPs:   a.localIPs,
		Hostname:   sysutil.Hostname(),
		Movies:     cfg.ShowMovies,
		Episodes:   cfg.ShowEpisodes,
		Music:      cfg.ShowMusic,
	}
}

// accountsFor turns account ids into filter accounts.
func accountsFor(cfg config.Config, ids []int64) []presence.Account {
	var accounts []presence.Account
	for _, id := range ids {
		acc := presence.Account{ID: id}
		if id == cfg.AccountID {
			acc.Owner = true
			acc.Names = []string{cfg.AccountName, cfg.AccountTitle}
		} else {
			for _, k := range cfg.KnownAccounts {
				if k.ID == id {
					acc.Names = []string{k.Title}
				}
			}
		}
		accounts = append(accounts, acc)
	}
	return accounts
}

// rememberDevices records the devices anyone in the Plex Home plays on
// (not friends the server is shared with), so they can be ticked in the
// tray's Devices menu.
func (a *App) rememberDevices(cands []presence.Candidate, f presence.Filter) {
	cfg := a.store.Get()
	home := []int64{cfg.AccountID}
	for _, k := range cfg.KnownAccounts {
		if k.ID != cfg.AccountID {
			home = append(home, k.ID)
		}
	}
	f.Accounts = accountsFor(cfg, home)
	now := time.Now()
	var seen []config.Device
	for _, c := range cands {
		p := c.Session.Player
		if p.MachineIdentifier == "" || f.OnThisPC(p) || !f.UserMatches(c) {
			continue
		}
		d := config.Device{ID: p.MachineIdentifier, Name: p.Title, Product: p.Product, LastSeen: now}
		if d.Name == "" {
			d.Name = p.Product
		}
		if cfg.RememberDevice(d) {
			seen = append(seen, d)
		}
	}
	if len(seen) == 0 {
		return
	}
	if err := a.store.Update(func(c *config.Config) {
		for _, d := range seen {
			c.RememberDevice(d)
		}
	}); err != nil {
		log.Printf("config: %v", err)
		return
	}
	a.notifyKnown()
}

// refreshAccounts reloads the Plex Home member list for the Accounts menu.
func (a *App) refreshAccounts(ctx context.Context, token string) {
	cfg := a.store.Get()
	known := []config.Account{{ID: cfg.AccountID, Title: firstNonEmpty(cfg.AccountTitle, cfg.AccountName), Admin: true}}
	users, err := a.plex.HomeUsers(ctx, token)
	if err != nil {
		log.Printf("plex: home users: %v", err)
	}
	for _, u := range users {
		if u.ID == cfg.AccountID || u.ID == 0 {
			continue
		}
		known = append(known, config.Account{ID: u.ID, Title: firstNonEmpty(u.Title, u.Username), Admin: u.Admin})
	}
	if slices.Equal(known, cfg.KnownAccounts) {
		return
	}
	if err := a.store.Update(func(c *config.Config) { c.KnownAccounts = known }); err != nil {
		log.Printf("config: %v", err)
		return
	}
	a.notifyKnown()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// OnKnownChange registers a callback for when the remembered devices or
// accounts change (to rebuild the tray menus).
func (a *App) OnKnownChange(fn func()) {
	a.mu.Lock()
	a.knownListeners = append(a.knownListeners, fn)
	a.mu.Unlock()
}

func (a *App) notifyKnown() {
	a.mu.Lock()
	ls := append([]func(){}, a.knownListeners...)
	a.mu.Unlock()
	for _, fn := range ls {
		fn()
	}
}

// logIgnored explains, once per session, why something playing is not shown.
// This is the first thing to check when the presence does not appear.
func (a *App) logIgnored(cands []presence.Candidate, f presence.Filter) {
	for _, c := range cands {
		key := presence.Key(c)
		if a.ignored[key] || f.Match(c) {
			continue
		}
		a.ignored[key] = true
		p := c.Session.Player
		log.Printf("ignoring %s %q: user %q (id %s), player %q %q at %s",
			c.Session.Type, title(c.Session), c.Session.User.Title, c.Session.User.ID, p.Product, p.Title, p.Address)
	}
	if len(a.ignored) > 200 {
		a.ignored = map[string]bool{}
	}
}

// dropGhosts removes sessions whose player has gone away without telling the
// server: playing but frozen for presence.StaleAfter, or reported by this PC
// while no Plex app is running here.
func (a *App) dropGhosts(cands []presence.Candidate, f presence.Filter) []presence.Candidate {
	// Running inside Plex (the mod): the player is obviously present, and the
	// "is Plex running?" check would misfire because this process is Plex.
	if a.hosted {
		return cands
	}
	appChecked, appRunning := false, true
	out := cands[:0:0]
	for _, c := range cands {
		if f.Match(c) && f.OnThisPC(c.Session.Player) {
			if !appChecked {
				appChecked, appRunning = true, sysutil.PlexAppRunning()
			}
			if !appRunning {
				a.logGhost(presence.Key(c), c, "the Plex app is not running")
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

func (a *App) logGhost(key string, c presence.Candidate, why string) {
	if a.ghosts == nil {
		a.ghosts = map[string]bool{}
	}
	if a.ghosts[key] {
		return
	}
	if len(a.ghosts) > 200 {
		a.ghosts = map[string]bool{}
	}
	a.ghosts[key] = true
	log.Printf("ignoring %s %q: still listed as playing but %s", c.Session.Type, title(c.Session), why)
}
