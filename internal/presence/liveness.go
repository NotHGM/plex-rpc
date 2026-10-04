package presence

import "time"

// StaleAfter is how long a session may claim to be playing without its
// position moving before it is treated as gone. Players report progress
// every few seconds; a player that was closed mid-playback never sends a
// "stopped" event, so the server keeps listing it as playing for a while.
const StaleAfter = 30 * time.Second

// Liveness spots sessions left behind by players that quit without
// stopping playback.
type Liveness struct {
	seen map[string]*progress
}

type progress struct {
	offset    int64
	state     string
	changedAt time.Time
	lastSeen  time.Time
}

// Stale records the session and reports whether it says "playing" but has
// not advanced for StaleAfter.
func (l *Liveness) Stale(key, state string, offset int64, now time.Time) bool {
	if l.seen == nil {
		l.seen = map[string]*progress{}
	}
	p, ok := l.seen[key]
	if !ok || p.offset != offset || p.state != state {
		p = &progress{offset: offset, state: state, changedAt: now}
		l.seen[key] = p
	}
	p.lastSeen = now
	return state == "playing" && now.Sub(p.changedAt) >= StaleAfter
}

// Prune forgets sessions not seen since before cutoff.
func (l *Liveness) Prune(cutoff time.Time) {
	for k, p := range l.seen {
		if p.lastSeen.Before(cutoff) {
			delete(l.seen, k)
		}
	}
}
