package plex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Session is one entry from /status/sessions. Only the fields plex-rpc uses
// are decoded.
type Session struct {
	Type                 string      `json:"type"` // movie, episode, track, clip
	SessionKey           string      `json:"sessionKey"`
	RatingKey            string      `json:"ratingKey"`
	ParentRatingKey      string      `json:"parentRatingKey"`
	GrandparentRatingKey string      `json:"grandparentRatingKey"`
	GUID                 string      `json:"guid"`
	ParentGUID           string      `json:"parentGuid"`
	GrandparentGUID      string      `json:"grandparentGuid"`
	Title                string      `json:"title"`
	ParentTitle          string      `json:"parentTitle"`
	GrandparentTitle     string      `json:"grandparentTitle"`
	OriginalTitle        string      `json:"originalTitle"`
	LibrarySectionTitle  string      `json:"librarySectionTitle"`
	Index                Int         `json:"index"`
	ParentIndex          Int         `json:"parentIndex"`
	Year                 Int         `json:"year"`
	ParentYear           Int         `json:"parentYear"`
	Duration             Int         `json:"duration"`
	ViewOffset           Int         `json:"viewOffset"`
	Thumb                string      `json:"thumb"`
	ParentThumb          string      `json:"parentThumb"`
	GrandparentThumb     string      `json:"grandparentThumb"`
	Live                 Int         `json:"live"`
	Genre                []Tag       `json:"Genre"`
	Director             []Tag       `json:"Director"`
	User                 SessionUser `json:"User"`
	Player               Player      `json:"Player"`
}

// Tag is a Plex tag such as a genre or director.
type Tag struct {
	Tag string `json:"tag"`
}

// SessionUser is the user block inside a session. Its id is the server-local
// account id, which is "1" for the server owner.
type SessionUser struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Player describes the device that is playing.
type Player struct {
	Address           string `json:"address"`
	MachineIdentifier string `json:"machineIdentifier"`
	Platform          string `json:"platform"`
	Product           string `json:"product"`
	State             string `json:"state"` // playing, paused, buffering
	Title             string `json:"title"` // device name
	Local             bool   `json:"local"`
}

// Int decodes numbers that Plex sometimes sends as strings.
type Int int64

func (i *Int) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		*i = 0
		return nil
	}
	*i = Int(n)
	return nil
}

type sessionsResponse struct {
	MediaContainer struct {
		Metadata []Session `json:"Metadata"`
	} `json:"MediaContainer"`
}

// ParseSessions decodes a /status/sessions body.
func ParseSessions(body []byte) ([]Session, error) {
	var r sessionsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return r.MediaContainer.Metadata, nil
}

// Sessions returns everything currently playing on srv.
func (c *Client) Sessions(ctx context.Context, srv Server) ([]Session, error) {
	var r sessionsResponse
	if err := c.doJSON(ctx, http.MethodGet, srv.URI+"/status/sessions", srv.Token, &r); err != nil {
		return nil, err
	}
	return r.MediaContainer.Metadata, nil
}

// Metadata is the subset of /library/metadata/{id} plex-rpc needs.
type Metadata struct {
	RatingKey string `json:"ratingKey"`
	GUID      string `json:"guid"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Guids     []struct {
		ID string `json:"id"`
	} `json:"Guid"`
}

// ExternalID returns the id for a scheme such as "imdb" or "tmdb".
func (m *Metadata) ExternalID(scheme string) string {
	prefix := scheme + "://"
	for _, g := range m.Guids {
		if strings.HasPrefix(g.ID, prefix) {
			return strings.TrimPrefix(g.ID, prefix)
		}
	}
	return ""
}

// Metadata fetches an item with its external guids.
func (c *Client) Metadata(ctx context.Context, srv Server, ratingKey string) (*Metadata, error) {
	var r struct {
		MediaContainer struct {
			Metadata []Metadata `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	u := srv.URI + "/library/metadata/" + url.PathEscape(ratingKey) + "?includeGuids=1"
	if err := c.doJSON(ctx, http.MethodGet, u, srv.Token, &r); err != nil {
		return nil, err
	}
	if len(r.MediaContainer.Metadata) == 0 {
		return nil, ErrNotFound
	}
	return &r.MediaContainer.Metadata[0], nil
}

// TranscodedImageURL returns a server URL that renders thumb at the given
// size. The URL contains the token and must not be logged.
func TranscodedImageURL(srv Server, thumb string, size int) string {
	v := url.Values{}
	v.Set("width", strconv.Itoa(size))
	v.Set("height", strconv.Itoa(size))
	v.Set("minSize", "1")
	v.Set("upscale", "1")
	v.Set("url", thumb)
	v.Set("X-Plex-Token", srv.Token)
	return srv.URI + "/photo/:/transcode?" + v.Encode()
}

// Get downloads a URL with Plex headers (used for images).
func (c *Client) Get(ctx context.Context, rawURL, token string) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodGet, rawURL, token)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/jpeg,image/*")
	return c.HTTP.Do(req)
}

// ProviderPoster looks up the public poster of a plex:// guid on the Plex
// metadata provider. The returned URL is on metadata-static.plex.tv and can
// be shown by Discord without exposing the user's server.
func (c *Client) ProviderPoster(ctx context.Context, guid, token string) (string, error) {
	id, ok := strings.CutPrefix(guid, "plex://")
	if !ok {
		return "", ErrNotFound
	}
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	var r struct {
		MediaContainer struct {
			Metadata []struct {
				Thumb string `json:"thumb"`
				Image []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"Image"`
			} `json:"Metadata"`
		} `json:"MediaContainer"`
	}
	u := "https://metadata.provider.plex.tv/library/metadata/" + url.PathEscape(id)
	if err := c.doJSON(ctx, http.MethodGet, u, token, &r); err != nil {
		return "", err
	}
	if len(r.MediaContainer.Metadata) == 0 {
		return "", ErrNotFound
	}
	m := r.MediaContainer.Metadata[0]
	if strings.HasPrefix(m.Thumb, "https://") {
		return m.Thumb, nil
	}
	for _, img := range m.Image {
		if img.Type == "coverPoster" && strings.HasPrefix(img.URL, "https://") {
			return img.URL, nil
		}
	}
	return "", ErrNotFound
}
