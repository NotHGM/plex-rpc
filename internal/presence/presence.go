package presence

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/NotHGM/plex-rpc/internal/discord"
	"github.com/NotHGM/plex-rpc/internal/plex"
)

// Asset keys uploaded to the Discord application (see README).
const (
	AssetLogo  = "plex"
	AssetPlay  = "play"
	AssetPause = "pause"
)

// Discord limits text fields to 2..128 characters.
const maxText = 128

// Extras are looked up separately from the session (network calls).
type Extras struct {
	Poster  string
	Buttons []discord.Button
}

// Key identifies a playback item on a server.
func Key(c Candidate) string {
	return c.Server.MachineID + "/" + c.Session.SessionKey + "/" + c.Session.RatingKey
}

// Build converts a session into an activity. start is when playback would
// have begun at normal speed (Unix ms), used for the progress bar.
func Build(s plex.Session, ex Extras, start int64) *discord.Activity {
	a := &discord.Activity{Type: discord.TypeWatching, StatusDisplayType: discord.DisplayDetails}
	large := ""
	switch s.Type {
	case "episode":
		a.Details = s.GrandparentTitle
		a.State = episodeLine(s)
		large = s.GrandparentTitle
		if s.ParentTitle != "" {
			large += " · " + s.ParentTitle
		}
	case "track":
		a.Type = discord.TypeListening
		a.StatusDisplayType = discord.DisplayState
		a.Details = s.Title
		a.State = artist(s)
		large = s.ParentTitle
		if s.ParentYear > 0 {
			large = fmt.Sprintf("%s (%d)", large, s.ParentYear)
		}
	default: // movie, clip
		a.Details = s.Title
		a.State = movieLine(s)
		large = s.Title
		if s.Year > 0 {
			large = fmt.Sprintf("%s (%d)", s.Title, s.Year)
		}
	}
	if a.Details == "" {
		a.Details = s.Title
	}

	assets := &discord.Assets{LargeImage: ex.Poster, LargeText: large}
	if assets.LargeImage == "" {
		assets.LargeImage = AssetLogo
	}
	paused := s.Player.State == "paused"
	if paused {
		assets.SmallImage, assets.SmallText = AssetPause, "Paused"
	} else {
		assets.SmallImage, assets.SmallText = AssetPlay, "Playing"
	}
	if s.Player.Product != "" {
		assets.SmallText += " on " + s.Player.Product
	}
	a.Assets = assets

	if !paused && start > 0 {
		a.Timestamps = &discord.Timestamps{Start: start}
		if s.Duration > 0 && s.Live == 0 {
			a.Timestamps.End = start + int64(s.Duration)
		}
	}
	a.Buttons = ex.Buttons

	a.Details = clamp(a.Details)
	a.State = clamp(a.State)
	assets.LargeText = clamp(assets.LargeText)
	assets.SmallText = clamp(assets.SmallText)
	return a
}

func episodeLine(s plex.Session) string {
	var b strings.Builder
	if s.ParentIndex > 0 && s.Index > 0 {
		fmt.Fprintf(&b, "S%02dE%02d", s.ParentIndex, s.Index)
	} else if s.Index > 0 {
		fmt.Fprintf(&b, "Episode %d", s.Index)
	}
	if s.Title != "" {
		if b.Len() > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(s.Title)
	}
	return b.String()
}

func artist(s plex.Session) string {
	if s.OriginalTitle != "" {
		return s.OriginalTitle // track artist, e.g. a featured artist
	}
	return s.GrandparentTitle
}

func movieLine(s plex.Session) string {
	var parts []string
	if s.Year > 0 {
		parts = append(parts, fmt.Sprint(s.Year))
	}
	var genres []string
	for i, g := range s.Genre {
		if i == 2 {
			break
		}
		genres = append(genres, g.Tag)
	}
	if len(genres) > 0 {
		parts = append(parts, strings.Join(genres, ", "))
	}
	if s.Duration > 0 {
		parts = append(parts, runtime(time.Duration(s.Duration)*time.Millisecond))
	}
	if len(parts) == 0 {
		return s.LibrarySectionTitle
	}
	return strings.Join(parts, " · ")
}

func runtime(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// clamp fits s into Discord's text limits.
func clamp(s string) string {
	s = strings.TrimSpace(s)
	switch n := utf8.RuneCountInString(s); {
	case n == 0:
		return ""
	case n == 1:
		return s + "⠀" // Discord requires at least two characters
	case n > maxText:
		r := []rune(s)
		return string(r[:maxText-1]) + "…"
	}
	return s
}

// Changed reports whether next differs enough from prev to be sent. Small
// timestamp jitter caused by the client's progress reports is ignored.
func Changed(prev, next *discord.Activity) bool {
	if prev == nil || next == nil {
		return prev != next
	}
	if prev.Type != next.Type || prev.StatusDisplayType != next.StatusDisplayType ||
		prev.Details != next.Details || prev.State != next.State {
		return true
	}
	if (prev.Assets == nil) != (next.Assets == nil) || (prev.Assets != nil && *prev.Assets != *next.Assets) {
		return true
	}
	if len(prev.Buttons) != len(next.Buttons) {
		return true
	}
	for i := range prev.Buttons {
		if prev.Buttons[i] != next.Buttons[i] {
			return true
		}
	}
	pt, nt := prev.Timestamps, next.Timestamps
	if (pt == nil) != (nt == nil) {
		return true
	}
	if pt != nil {
		const drift = 10_000 // ms
		if abs(pt.Start-nt.Start) > drift || abs(pt.End-nt.End) > drift || (pt.End == 0) != (nt.End == 0) {
			return true
		}
	}
	return false
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
