// Package plexapp reads what the Plex desktop app on this computer is playing
// from the app's own log.
//
// This is used for servers the user does not own: only a server's owner can
// list its sessions, but the desktop app logs a timeline entry for whatever
// it plays, whichever server the item comes from.
package plexapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Timeline is the desktop app's most recent playback report.
type Timeline struct {
	RatingKey string
	State     string // playing, paused, buffering, stopped
	Time      int64  // position, ms
	Duration  int64  // ms
	At        time.Time
}

// tailSize is how much of the end of the log is read. The app logs a
// timeline every 10 seconds during playback, a small fraction of this.
const tailSize = 512 << 10

const (
	marker     = "[Player] Timeline, "
	timeLayout = "Jan 2, 2006 15:04:05.000"
)

// LogPath returns the desktop app's current log file, or "" when unknown.
func LogPath() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "Plex", "Logs", "Plex.log")
}

// Reader returns the latest timeline from a log file, re-reading the file
// only when it has changed.
type Reader struct {
	Path string

	size int64
	mod  time.Time
	last *Timeline
}

// Latest returns the newest timeline in the log, or nil when there is none.
func (r *Reader) Latest() (*Timeline, error) {
	if r.Path == "" {
		return nil, nil
	}
	f, err := openShared(r.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			r.size, r.last = 0, nil
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	// Stat the open file: Windows updates the directory entry of a file that
	// another process is writing only lazily.
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() == r.size && st.ModTime().Equal(r.mod) {
		return r.last, nil
	}
	off := max(st.Size()-tailSize, 0)
	buf, err := io.ReadAll(io.NewSectionReader(f, off, st.Size()-off))
	if err != nil {
		return nil, err
	}
	r.size, r.mod = st.Size(), st.ModTime()
	r.last = Parse(buf, time.Local)
	return r.last, nil
}

// Parse returns the last timeline entry in a chunk of log text. Entries look
// like this, with the JSON spread over the following lines:
//
//	Oct 11, 2026 13:11:57.266 [8864] INFO - [Web] [Player] Timeline, {
//	  "ratingKey": "62539",
//	  "state": "playing",
//	  "time": 242000,
//	  "duration": 1348000
//	}
func Parse(log []byte, loc *time.Location) *Timeline {
	for end := len(log); end > 0; {
		i := bytes.LastIndex(log[:end], []byte(marker))
		if i < 0 {
			return nil
		}
		end = i
		if t, ok := parseEntry(log, i, loc); ok {
			return t
		}
	}
	return nil
}

func parseEntry(log []byte, i int, loc *time.Location) (*Timeline, bool) {
	lineStart := bytes.LastIndexByte(log[:i], '\n') + 1
	line := log[lineStart:i]
	if len(line) < len(timeLayout) {
		return nil, false
	}
	// The timestamp is followed by " [thread]"; its length varies with the day.
	sp := bytes.Index(line, []byte(" ["))
	if sp < 0 {
		return nil, false
	}
	at, err := time.ParseInLocation(timeLayout, string(line[:sp]), loc)
	if err != nil {
		return nil, false
	}
	var v struct {
		RatingKey string      `json:"ratingKey"`
		State     string      `json:"state"`
		Time      json.Number `json:"time"`
		Duration  json.Number `json:"duration"`
	}
	dec := json.NewDecoder(bytes.NewReader(log[i+len(marker):]))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil || v.RatingKey == "" {
		return nil, false
	}
	pos, _ := v.Time.Int64()
	dur, _ := v.Duration.Int64()
	return &Timeline{RatingKey: v.RatingKey, State: v.State, Time: pos, Duration: dur, At: at}, true
}
