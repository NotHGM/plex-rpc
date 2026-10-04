// Package artwork finds a public image URL for a Plex item, because Discord
// can only display images it can download itself.
package artwork

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/NotHGM/plex-rpc/internal/plex"
)

const (
	// Discord rejects image URLs longer than this.
	maxURLLen = 256
	// How long a failed lookup is remembered before trying again.
	negativeTTL = 6 * time.Hour
	// Litterbox keeps uploads for 72h; refresh a little earlier.
	uploadTTL = 70 * time.Hour
	// Provider and iTunes URLs are stable.
	stableTTL = 30 * 24 * time.Hour
)

type entry struct {
	URL     string    `json:"url"`
	Expires time.Time `json:"expires"`
}

// Resolver looks up and caches artwork URLs.
type Resolver struct {
	plex      *plex.Client
	cachePath string
	http      *http.Client

	mu    sync.Mutex
	cache map[string]entry
	dirty bool
}

// New creates a resolver persisting its cache at cachePath.
func New(pc *plex.Client, cachePath string) *Resolver {
	r := &Resolver{
		plex:      pc,
		cachePath: cachePath,
		http:      &http.Client{Timeout: 20 * time.Second},
		cache:     map[string]entry{},
	}
	if b, err := os.ReadFile(cachePath); err == nil {
		_ = json.Unmarshal(b, &r.cache)
	}
	return r
}

// Request describes what to look up.
type Request struct {
	Server plex.Server
	// GUID is the plex:// guid of the item whose poster to use (the show for
	// episodes, the album for tracks).
	GUID string
	// Thumb is the server path of the same image, used for uploads.
	Thumb string
	// Artist and Album enable the iTunes fallback for music.
	Artist, Album string
	// AllowUpload permits uploading Thumb to a temporary public host.
	AllowUpload bool
	// UserToken authenticates against the Plex metadata provider.
	UserToken string
}

// Lookup returns an https URL, or "" when nothing suitable exists.
func (r *Resolver) Lookup(ctx context.Context, req Request) string {
	if req.GUID != "" {
		if u, ok := r.cached(ctx, "plex:"+req.GUID, stableTTL, func() (string, error) {
			return r.plex.ProviderPoster(ctx, req.GUID, req.UserToken)
		}); ok {
			return u
		}
	}
	if req.Artist != "" && req.Album != "" {
		key := "itunes:" + strings.ToLower(req.Artist+"\x00"+req.Album)
		if u, ok := r.cached(ctx, key, stableTTL, func() (string, error) {
			return r.itunes(ctx, req.Artist, req.Album)
		}); ok {
			return u
		}
	}
	if req.AllowUpload && req.Thumb != "" {
		key := "upload:" + req.Server.MachineID + req.Thumb
		if u, ok := r.cached(ctx, key, uploadTTL, func() (string, error) {
			return r.upload(ctx, req.Server, req.Thumb)
		}); ok {
			return u
		}
	}
	return ""
}

// cached returns a cached URL or runs fetch and stores the result. Failures
// are cached briefly so a missing poster is not looked up every poll.
func (r *Resolver) cached(ctx context.Context, key string, ttl time.Duration, fetch func() (string, error)) (string, bool) {
	r.mu.Lock()
	e, hit := r.cache[key]
	r.mu.Unlock()
	if hit && time.Now().Before(e.Expires) {
		return e.URL, e.URL != ""
	}
	u, err := fetch()
	if err != nil || len(u) > maxURLLen || !strings.HasPrefix(u, "https://") {
		if ctx.Err() != nil {
			return "", false // cancelled, do not remember
		}
		if err != nil && !errors.Is(err, plex.ErrNotFound) {
			log.Printf("artwork: %s: %v", strings.SplitN(key, ":", 2)[0], err)
		}
		u, ttl = "", negativeTTL
	}
	r.mu.Lock()
	r.cache[key] = entry{URL: u, Expires: time.Now().Add(ttl)}
	r.dirty = true
	r.mu.Unlock()
	return u, u != ""
}

// Save writes the cache to disk if it changed.
func (r *Resolver) Save() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.dirty {
		return
	}
	now := time.Now()
	for k, e := range r.cache {
		if now.After(e.Expires) {
			delete(r.cache, k)
		}
	}
	b, err := json.MarshalIndent(r.cache, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(r.cachePath, b, 0o600); err == nil {
		r.dirty = false
	}
}

// itunes finds album art through the public iTunes Search API.
func (r *Resolver) itunes(ctx context.Context, artist, album string) (string, error) {
	v := url.Values{}
	v.Set("term", artist+" "+album)
	v.Set("entity", "album")
	v.Set("limit", "5")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://itunes.apple.com/search?"+v.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("itunes: %s", resp.Status)
	}
	var out struct {
		Results []struct {
			ArtistName     string `json:"artistName"`
			CollectionName string `json:"collectionName"`
			ArtworkURL100  string `json:"artworkUrl100"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	for _, res := range out.Results {
		if res.ArtworkURL100 == "" || !strings.Contains(norm(res.ArtistName), norm(artist)) {
			continue
		}
		if !strings.Contains(norm(res.CollectionName), norm(album)) && !strings.Contains(norm(album), norm(res.CollectionName)) {
			continue
		}
		return strings.Replace(res.ArtworkURL100, "100x100bb", "512x512bb", 1), nil
	}
	return "", plex.ErrNotFound
}

// upload copies a server thumbnail to litterbox.catbox.moe (deleted after 72h).
func (r *Resolver) upload(ctx context.Context, srv plex.Server, thumb string) (string, error) {
	resp, err := r.plex.Get(ctx, plex.TranscodedImageURL(srv, thumb, 512), srv.Token)
	if err != nil {
		return "", err
	}
	img, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || len(img) == 0 {
		return "", fmt.Errorf("thumbnail: %s", resp.Status)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("reqtype", "fileupload")
	_ = mw.WriteField("time", "72h")
	fw, err := mw.CreateFormFile("fileToUpload", "cover.jpg")
	if err != nil {
		return "", err
	}
	_, _ = fw.Write(img)
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://litterbox.catbox.moe/resources/internals/api.php", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	up, err := r.http.Do(req)
	if err != nil {
		return "", err
	}
	defer up.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(up.Body, 1024))
	u := strings.TrimSpace(string(out))
	if up.StatusCode != http.StatusOK || !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("litterbox: %s: %s", up.Status, u)
	}
	return u, nil
}
