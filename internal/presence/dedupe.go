package presence

import (
	"strconv"

	"github.com/NotHGM/plex-rpc/internal/plex"
)

// DedupeByDevice collapses several sessions from the same player device down to
// one: the newest, by session key.
//
// Plex sometimes keeps a previous ("zombie") session listed as still playing
// after another has started, or alongside the real one while paused. That makes
// the shown title and play/pause state flip between polls. The real, current
// session always has the higher session key, so keeping the highest per device
// removes the stale duplicates.
func DedupeByDevice(cands []Candidate) []Candidate {
	idx := make(map[string]int, len(cands))
	out := make([]Candidate, 0, len(cands))
	for _, c := range cands {
		dev := c.Session.Player.MachineIdentifier
		if dev == "" {
			out = append(out, c)
			continue
		}
		if i, ok := idx[dev]; ok {
			if sessionKey(c.Session) > sessionKey(out[i].Session) {
				out[i] = c
			}
			continue
		}
		idx[dev] = len(out)
		out = append(out, c)
	}
	return out
}

func sessionKey(s plex.Session) int64 {
	n, _ := strconv.ParseInt(s.SessionKey, 10, 64)
	return n
}
