package app

import (
	"context"
	"errors"
	"log"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/plex"
	"github.com/NotHGM/plex-rpc/internal/plexapp"
	"github.com/NotHGM/plex-rpc/internal/presence"
)

// timelineStale is how old the app's last "playing" timeline may be before
// playback is treated as over. The app logs one every 10 seconds.
const timelineStale = 30 * time.Second

// sharedItem caches which shared server an item was found on.
type sharedItem struct {
	key  string // ratingKey/duration
	cand *presence.Candidate
	at   time.Time
}

// fromPlexApp returns what the Plex app on this PC is playing from a server a
// friend has shared. Only a server's owner can list its sessions, so this
// reads the app's own log and looks the item up on the shared servers.
func (a *App) fromPlexApp(ctx context.Context, cfg config.Config, cands []presence.Candidate, f presence.Filter) *presence.Candidate {
	if !cfg.ShareThisPC || !slices.ContainsFunc(a.servers, func(s plex.Server) bool { return !s.Owned }) {
		return nil
	}
	// An owned server already reports what this PC plays from it.
	for _, c := range cands {
		if f.Match(c) && f.OnThisPC(c.Session.Player) {
			return nil
		}
	}
	t, err := a.appLog.Latest()
	if err != nil {
		if msg := err.Error(); msg != a.appLogErr {
			a.appLogErr = msg
			log.Printf("plex app log: %v", err)
		}
		return nil
	}
	a.appLogErr = ""
	if t == nil || t.State == "stopped" {
		return nil
	}
	age := time.Since(t.At)
	paused := t.State == "paused"
	if !paused && age > timelineStale {
		return nil
	}
	found := a.resolveShared(ctx, t)
	if found == nil {
		return nil
	}

	c := *found
	s := &c.Session
	// The log is written every 10 seconds; extrapolate between entries so the
	// position keeps moving while playing.
	pos := t.Time
	if !paused && age > 0 {
		pos += age.Milliseconds()
	}
	if s.Duration > 0 {
		pos = min(pos, int64(s.Duration))
	}
	s.ViewOffset = plex.Int(pos)
	s.Player = plex.Player{Product: "Plex for Windows", State: t.State, Title: f.Hostname}
	s.User = plex.SessionUser{ID: strconv.FormatInt(cfg.AccountID, 10), Title: firstNonEmpty(cfg.AccountTitle, cfg.AccountName)}
	return &c
}

// resolveShared finds the timeline's item on the shared servers. Rating keys
// are only unique per server, so the item's duration must match too.
func (a *App) resolveShared(ctx context.Context, t *plexapp.Timeline) *presence.Candidate {
	key := t.RatingKey + "/" + strconv.FormatInt(t.Duration, 10)
	if a.shared.key == key && (a.shared.cand != nil || time.Since(a.shared.at) < time.Minute) {
		return a.shared.cand
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	found := make([]*plex.Session, len(a.servers))
	var wg sync.WaitGroup
	for i, srv := range a.servers {
		if srv.Owned {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := a.plex.Item(ctx, srv, t.RatingKey)
			switch {
			case errors.Is(err, plex.ErrNotFound):
			case err != nil:
				log.Printf("plex: %s: item %s: %v", srv.Name, t.RatingKey, err)
			case durationMatches(int64(s.Duration), t.Duration):
				found[i] = s
			}
		}()
	}
	wg.Wait()

	a.shared = sharedItem{key: key, at: time.Now()}
	for i, s := range found {
		if s != nil {
			a.shared.cand = &presence.Candidate{Server: a.servers[i], Session: *s}
			return a.shared.cand
		}
	}
	log.Printf("plex app is playing item %s, but no shared server has it", t.RatingKey)
	return nil
}

func durationMatches(item, timeline int64) bool {
	if item == 0 || timeline == 0 {
		return true
	}
	return abs(item-timeline) <= max(5000, timeline/50)
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
