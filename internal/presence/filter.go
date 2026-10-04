// Package presence decides which Plex session to show and turns it into a
// Discord activity.
package presence

import (
	"strconv"
	"strings"

	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/plex"
)

// Candidate is a session together with the server it is playing from.
type Candidate struct {
	Server  plex.Server
	Session plex.Session
}

// Filter selects the sessions that belong to the user on this computer.
type Filter struct {
	Mode         string // config.PlayerThisPC or config.PlayerAny
	AccountID    int64
	AccountNames []string
	LocalIPs     map[string]bool
	Hostname     string

	Movies, Episodes, Music bool
}

// Match reports whether s should be shown.
func (f Filter) Match(c Candidate) bool {
	return f.kindEnabled(c.Session.Type) && f.userMatches(c.Session.User, c.Server.Owned) && f.playerMatches(c.Session.Player)
}

func (f Filter) kindEnabled(kind string) bool {
	switch kind {
	case "movie", "clip":
		return f.Movies
	case "episode":
		return f.Episodes
	case "track":
		return f.Music
	default:
		return false
	}
}

func (f Filter) userMatches(u plex.SessionUser, owned bool) bool {
	if f.AccountID != 0 && u.ID == strconv.FormatInt(f.AccountID, 10) {
		return true
	}
	// The server owner always has local account id 1.
	if owned && u.ID == "1" {
		return true
	}
	for _, n := range f.AccountNames {
		if n != "" && strings.EqualFold(u.Title, n) {
			return true
		}
	}
	return false
}

func (f Filter) playerMatches(p plex.Player) bool {
	if f.Mode == config.PlayerAny {
		return true
	}
	if p.Address != "" && f.LocalIPs[p.Address] {
		return true
	}
	// The desktop app reports the computer name as its device title. This
	// also covers servers that see the client through NAT or Docker.
	return f.Hostname != "" && strings.EqualFold(p.Title, f.Hostname)
}

// Pick returns the session to display: the first playing one, otherwise the
// first paused one.
func Pick(cands []Candidate, f Filter) *Candidate {
	var paused *Candidate
	for i := range cands {
		c := &cands[i]
		if !f.Match(*c) {
			continue
		}
		if c.Session.Player.State != "paused" {
			return c
		}
		if paused == nil {
			paused = c
		}
	}
	return paused
}
