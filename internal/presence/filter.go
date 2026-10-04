// Package presence decides which Plex session to show and turns it into a
// Discord activity.
package presence

import (
	"strconv"
	"strings"

	"github.com/NotHGM/plex-rpc/internal/plex"
)

// Candidate is a session together with the server it is playing from.
type Candidate struct {
	Server  plex.Server
	Session plex.Session
}

// Account is a plex.tv account whose playback counts as the user's.
type Account struct {
	ID    int64
	Names []string
	// Owner is the signed-in account; it owns the server and appears there
	// with local id "1".
	Owner bool
}

// Filter selects the sessions to show: the chosen accounts, on the chosen
// devices.
type Filter struct {
	Accounts   []Account
	ThisPC     bool
	AllDevices bool
	Devices    map[string]bool // machine identifiers
	LocalIPs   map[string]bool
	Hostname   string

	Movies, Episodes, Music bool
}

// Match reports whether s should be shown.
func (f Filter) Match(c Candidate) bool {
	return f.kindEnabled(c.Session.Type) && f.UserMatches(c) && f.playerMatches(c.Session.Player)
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

// UserMatches reports whether the session belongs to one of the accounts.
func (f Filter) UserMatches(c Candidate) bool {
	u := c.Session.User
	for _, a := range f.Accounts {
		if a.ID != 0 && u.ID == strconv.FormatInt(a.ID, 10) {
			return true
		}
		if a.Owner && c.Server.Owned && u.ID == "1" {
			return true
		}
		for _, n := range a.Names {
			if n != "" && strings.EqualFold(u.Title, n) {
				return true
			}
		}
	}
	return false
}

func (f Filter) playerMatches(p plex.Player) bool {
	switch {
	case f.AllDevices:
		return true
	case p.MachineIdentifier != "" && f.Devices[p.MachineIdentifier]:
		return true
	case f.ThisPC:
		return f.OnThisPC(p)
	}
	return false
}

// OnThisPC reports whether the player is this computer.
func (f Filter) OnThisPC(p plex.Player) bool {
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
