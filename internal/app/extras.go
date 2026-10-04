package app

import (
	"context"
	"log"
	"time"

	"github.com/NotHGM/plex-rpc/internal/artwork"
	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/discord"
	"github.com/NotHGM/plex-rpc/internal/plex"
	"github.com/NotHGM/plex-rpc/internal/presence"
)

// extrasFor returns the poster and buttons for an item. Lookups run in the
// background so the presence appears straight away with the Plex logo and
// is updated once the artwork is known.
func (a *App) extrasFor(ctx context.Context, c presence.Candidate, cfg config.Config, token string) presence.Extras {
	key := c.Server.MachineID + "/" + c.Session.RatingKey
	if cfg.UploadArtwork {
		key += "/upload"
	}
	if !cfg.ShowButtons {
		key += "/nobuttons"
	}
	a.extrasMu.Lock()
	defer a.extrasMu.Unlock()
	if ex, ok := a.extras[key]; ok {
		if ex == nil {
			return presence.Extras{}
		}
		return *ex
	}
	if len(a.extras) > 100 {
		a.extras = map[string]*presence.Extras{}
	}
	a.extras[key] = nil
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ex := a.lookupExtras(ctx, c, cfg, token)
		a.extrasMu.Lock()
		a.extras[key] = &ex
		a.extrasMu.Unlock()
		a.Wake()
	}()
	return presence.Extras{}
}

func (a *App) lookupExtras(ctx context.Context, c presence.Candidate, cfg config.Config, token string) presence.Extras {
	s := c.Session
	req := artwork.Request{Server: c.Server, AllowUpload: cfg.UploadArtwork, UserToken: token}
	metaKey, tmdbKind := s.RatingKey, "movie"
	switch s.Type {
	case "episode":
		req.GUID, req.Thumb = s.GrandparentGUID, s.GrandparentThumb
		metaKey, tmdbKind = s.GrandparentRatingKey, "tv"
	case "track":
		req.GUID, req.Thumb = s.ParentGUID, s.ParentThumb
		req.Artist, req.Album = s.GrandparentTitle, s.ParentTitle
		metaKey = ""
	default:
		req.GUID, req.Thumb = s.GUID, s.Thumb
	}
	if req.Thumb == "" {
		req.Thumb = s.Thumb
	}

	var ex presence.Extras
	ex.Poster = a.art.Lookup(ctx, req)

	if cfg.ShowButtons && metaKey != "" {
		m, err := a.plex.Metadata(ctx, c.Server, metaKey)
		if err != nil {
			log.Printf("plex: metadata %s: %v", metaKey, err)
		} else {
			ex.Buttons = buttons(m, tmdbKind)
		}
	}
	return ex
}

func buttons(m *plex.Metadata, tmdbKind string) []discord.Button {
	var out []discord.Button
	if id := m.ExternalID("imdb"); id != "" {
		out = append(out, discord.Button{Label: "IMDb", URL: "https://www.imdb.com/title/" + id + "/"})
	}
	if id := m.ExternalID("tmdb"); id != "" {
		out = append(out, discord.Button{Label: "TMDB", URL: "https://www.themoviedb.org/" + tmdbKind + "/" + id})
	}
	return out
}
