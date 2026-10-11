package plexapp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const sampleLog = `Oct 11, 2026 13:11:47.269 [8864] INFO - [Web] [Player] Timeline, {
  "ratingKey": "62539",
  "key": "/library/metadata/62539",
  "playbackTime": 74074,
  "playQueueItemID": "750638",
  "state": "playing",
  "hasMDE": 1,
  "time": 232000,
  "duration": 1348000
}
Oct 11, 2026 13:11:47.270 [8864] ERROR - [Web] Did not move header "accept" to query string.
Oct 1, 2026 13:11:57.266 [8864] INFO - [Web] [Player] Timeline, {
  "ratingKey": "62539",
  "key": "/library/metadata/62539",
  "state": "paused",
  "time": 242000,
  "duration": 1348000
}
Oct 11, 2026 13:11:58.879 [8864] INFO - [Web] [Connections] Testing all 1 connection(s) for [Loopback]
`

func TestParseLatest(t *testing.T) {
	got := Parse([]byte(sampleLog), time.UTC)
	if got == nil {
		t.Fatal("no timeline found")
	}
	want := Timeline{RatingKey: "62539", State: "paused", Time: 242000, Duration: 1348000,
		At: time.Date(2026, 10, 1, 13, 11, 57, 266e6, time.UTC)}
	if *got != want {
		t.Fatalf("got %+v, want %+v", *got, want)
	}
}

func TestParseSkipsTruncatedEntry(t *testing.T) {
	// The newest entry is cut off mid-write; the previous one is used.
	log := sampleLog + "Oct 11, 2026 13:12:07.266 [8864] INFO - [Web] [Player] Timeline, {\n  \"ratingKey\": \"62"
	got := Parse([]byte(log), time.UTC)
	if got == nil || got.State != "paused" {
		t.Fatalf("got %+v, want the paused entry", got)
	}
}

func TestParseNone(t *testing.T) {
	if got := Parse([]byte("Oct 11, 2026 13:11:58.879 [8864] INFO - nothing here\n"), time.UTC); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

func TestReaderRereadsOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Plex.log")
	r := Reader{Path: path}
	if got, err := r.Latest(); err != nil || got != nil {
		t.Fatalf("missing file: got %+v, %v", got, err)
	}
	if err := os.WriteFile(path, []byte(sampleLog), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := r.Latest()
	if err != nil || got == nil || got.Time != 242000 {
		t.Fatalf("got %+v, %v", got, err)
	}
	more := sampleLog + `Oct 11, 2026 13:12:07.266 [8864] INFO - [Web] [Player] Timeline, {
  "ratingKey": "62539",
  "state": "playing",
  "time": 252000,
  "duration": 1348000
}
`
	if err := os.WriteFile(path, []byte(more), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = r.Latest()
	if err != nil || got == nil || got.Time != 252000 || got.State != "playing" {
		t.Fatalf("after append: got %+v, %v", got, err)
	}
}
