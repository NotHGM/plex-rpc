package presence

import "time"

// Tracker keeps the per-session state needed across polls: a stable start
// time for the progress bar and how long playback has been paused.
type Tracker struct {
	key         string
	offset      int64
	start       int64
	state       string
	pausedSince time.Time
}

// Observe records the current session and returns the progress-bar start
// time (Unix ms) and how long it has been paused (0 while playing).
//
// The server only learns the position when the player reports it, so the
// start time is anchored when the reported offset changes instead of being
// recomputed from a stale offset on every poll.
func (t *Tracker) Observe(key, state string, offset int64, now time.Time) (start int64, paused time.Duration) {
	nowMs := now.UnixMilli()
	if key != t.key {
		*t = Tracker{key: key, offset: -1}
	}
	if offset != t.offset || state != t.state {
		t.offset = offset
		t.start = nowMs - offset
	}
	if state == "paused" {
		if t.state != "paused" || t.pausedSince.IsZero() {
			t.pausedSince = now
		}
		paused = now.Sub(t.pausedSince)
	} else {
		t.pausedSince = time.Time{}
	}
	t.state = state
	return t.start, paused
}

// Reset forgets the tracked session.
func (t *Tracker) Reset() { *t = Tracker{} }
