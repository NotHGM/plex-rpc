// Package config loads and saves the plex-rpc settings file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Player filter modes.
const (
	PlayerThisPC = "this_pc"
	PlayerAny    = "any"
)

// Config is the on-disk settings file. Fields are exported for JSON; use the
// Store methods to read and change them safely from several goroutines.
type Config struct {
	// DiscordClientID is the application ID from the Discord Developer Portal.
	// Empty means "use the ID baked into the binary".
	DiscordClientID string `json:"discord_client_id"`

	// PlexToken is the plex.tv auth token, encrypted with DPAPI on Windows.
	PlexToken string `json:"plex_token,omitempty"`
	// ClientIdentifier identifies this install to plex.tv. Generated once.
	ClientIdentifier string `json:"client_identifier"`
	// AccountID / AccountName / AccountTitle describe the signed-in plex.tv user.
	AccountID    int64  `json:"account_id,omitempty"`
	AccountName  string `json:"account_name,omitempty"`
	AccountTitle string `json:"account_title,omitempty"`

	// ServerURL forces a specific Plex Media Server (e.g. http://192.168.1.10:32400)
	// instead of discovering owned servers through plex.tv.
	ServerURL string `json:"server_url"`

	// PlayerFilter is "this_pc" (only playback on this computer) or "any".
	PlayerFilter string `json:"player_filter"`
	// ClearAfterPauseMinutes clears the presence after being paused this long.
	// 0 clears immediately on pause, -1 never clears.
	ClearAfterPauseMinutes int `json:"clear_after_pause_minutes"`
	// UploadArtwork uploads server thumbnails to litterbox.catbox.moe when no
	// public poster can be found (personal media, unmatched items).
	UploadArtwork bool `json:"upload_artwork"`
	// ShowButtons adds IMDb / TMDB buttons to the presence.
	ShowButtons bool `json:"show_buttons"`
	// ShowMusic / ShowMovies / ShowEpisodes toggle presence per media kind.
	ShowMusic    bool `json:"show_music"`
	ShowMovies   bool `json:"show_movies"`
	ShowEpisodes bool `json:"show_episodes"`
	// PollIntervalSeconds is how often the server is asked for sessions.
	PollIntervalSeconds int `json:"poll_interval_seconds"`
	// Disabled hides the presence without quitting (tray "Pause presence").
	Disabled bool `json:"disabled"`
}

func defaults() Config {
	return Config{
		PlayerFilter:           PlayerThisPC,
		ClearAfterPauseMinutes: 5,
		ShowButtons:            true,
		ShowMusic:              true,
		ShowMovies:             true,
		ShowEpisodes:           true,
		PollIntervalSeconds:    2,
	}
}

// Dir returns the settings directory (%APPDATA%\plex-rpc on Windows).
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "plex-rpc"), nil
}

// Store guards a Config and persists it.
type Store struct {
	mu      sync.Mutex
	path    string
	cfg     Config
	modTime time.Time
}

// Open loads the config from dir, creating it with defaults if missing.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dir, "config.json"), cfg: defaults()}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	changed := false
	if s.cfg.ClientIdentifier == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		s.cfg.ClientIdentifier = "plex-rpc-" + hex.EncodeToString(b)
		changed = true
	}
	if _, err := os.Stat(s.path); errors.Is(err, os.ErrNotExist) {
		changed = true
	}
	if changed {
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// Path is the location of config.json.
func (s *Store) Path() string { return s.path }

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	cfg := defaults()
	if err := json.Unmarshal(b, &cfg); err != nil {
		return fmt.Errorf("parse %s: %w", s.path, err)
	}
	s.normalize(&cfg)
	s.cfg = cfg
	if st, err := os.Stat(s.path); err == nil {
		s.modTime = st.ModTime()
	}
	return nil
}

func (s *Store) normalize(c *Config) {
	if c.PlayerFilter != PlayerAny {
		c.PlayerFilter = PlayerThisPC
	}
	if c.PollIntervalSeconds < 1 {
		c.PollIntervalSeconds = 1
	}
	if c.PollIntervalSeconds > 60 {
		c.PollIntervalSeconds = 60
	}
}

// ReloadIfChanged re-reads the file when it was edited by hand. It returns
// true when a reload happened.
func (s *Store) ReloadIfChanged() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := os.Stat(s.path)
	if err != nil {
		return false, err
	}
	if st.ModTime().Equal(s.modTime) {
		return false, nil
	}
	if err := s.load(); err != nil {
		// Keep the old config and stop retrying until the file changes again.
		s.modTime = st.ModTime()
		return false, err
	}
	return true, nil
}

// Get returns a copy of the current config.
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// Update applies fn to the config and saves it.
func (s *Store) Update(fn func(c *Config)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cfg)
	s.normalize(&s.cfg)
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	b, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	if st, err := os.Stat(s.path); err == nil {
		s.modTime = st.ModTime()
	}
	return nil
}

// Token returns the decrypted Plex token, or "" when signed out.
func (s *Store) Token() (string, error) {
	s.mu.Lock()
	enc := s.cfg.PlexToken
	s.mu.Unlock()
	if enc == "" {
		return "", nil
	}
	return unprotect(enc)
}

// SetToken encrypts and stores the Plex token. An empty token signs out.
func (s *Store) SetToken(token string) error {
	enc := ""
	if token != "" {
		var err error
		if enc, err = protect(token); err != nil {
			return err
		}
	}
	return s.Update(func(c *Config) { c.PlexToken = enc })
}
