package presence

import "time"

// Tracker keeps the per-session state needed across polls: a stable start
// time for the progress bar, and how long it has been since playback last
// moved forward.
type Tracker struct {
	key     string
	offset  int64
	start   int64
	movedAt time.Time
}

// Observe records the current session. It returns the progress-bar start time
// (Unix ms) and how long it has been since the reported position last changed.
//
// The server only learns the position when the player reports it, so the start
// time is anchored to the reported offset and only re-anchored when that offset
// changes. sinceMove grows whenever the position stops advancing — which is how
// a pause looks, including on players that keep reporting their state as
// "playing" while paused.
func (t *Tracker) Observe(key string, offset int64, now time.Time) (start int64, sinceMove time.Duration) {
	nowMs := now.UnixMilli()
	if key != t.key {
		*t = Tracker{key: key, offset: offset, start: nowMs - offset, movedAt: now}
		return t.start, 0
	}
	if offset != t.offset {
		t.offset = offset
		t.start = nowMs - offset
		t.movedAt = now
	}
	return t.start, now.Sub(t.movedAt)
}

// Reset forgets the tracked session.
func (t *Tracker) Reset() { *t = Tracker{} }
