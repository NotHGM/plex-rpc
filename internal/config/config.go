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
	"slices"
	"sync"
	"time"
)

// Device is a Plex player that has been seen playing on one of the shared
// accounts. Devices are remembered so they can be picked in the tray.
type Device struct {
	ID       string    `json:"id"` // Plex machine identifier
	Name     string    `json:"name"`
	Product  string    `json:"product,omitempty"`
	LastSeen time.Time `json:"last_seen"`
}

// Account is a plex.tv account in the user's Plex Home (the main account
// and its managed or local users).
type Account struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Admin bool   `json:"admin,omitempty"`
}

// maxKnownDevices bounds the remembered device list.
const maxKnownDevices = 25

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

	// ShareThisPC shares playback from the Plex app on this computer.
	ShareThisPC bool `json:"share_this_pc"`
	// ShareAllDevices shares playback from every device.
	ShareAllDevices bool `json:"share_all_devices"`
	// ShareDevices lists the machine identifiers of other devices to share.
	ShareDevices []string `json:"share_devices"`
	// ShareAccounts lists the plex.tv account ids whose playback counts as
	// yours. Unset (null) means only the signed-in account.
	ShareAccounts []int64 `json:"share_accounts"`

	// KnownDevices and KnownAccounts feed the tray menus.
	KnownDevices  []Device  `json:"known_devices,omitempty"`
	KnownAccounts []Account `json:"known_accounts,omitempty"`

	// PlayerFilter is the v0.1 setting ("this_pc" or "any"). It is migrated
	// to ShareAllDevices on load.
	PlayerFilter string `json:"player_filter,omitempty"`
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
		ShareThisPC:            true,
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
	if c.PlayerFilter == "any" {
		c.ShareAllDevices = true
	}
	c.PlayerFilter = ""
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

// SharedAccounts returns the account ids whose playback is shown.
func (c *Config) SharedAccounts() []int64 {
	if c.ShareAccounts == nil {
		return []int64{c.AccountID} // 0 when the account is unknown; still means "me"
	}
	return c.ShareAccounts
}

// SetAccountShared adds or removes an account from SharedAccounts.
func (c *Config) SetAccountShared(id int64, on bool) {
	ids := slices.Clone(c.SharedAccounts())
	ids = slices.DeleteFunc(ids, func(v int64) bool { return v == id })
	if on {
		ids = append(ids, id)
	}
	if ids == nil {
		ids = []int64{} // keep "nobody" distinct from the default
	}
	c.ShareAccounts = ids
}

// SetDeviceShared adds or removes a device from ShareDevices.
func (c *Config) SetDeviceShared(id string, on bool) {
	c.ShareDevices = slices.DeleteFunc(c.ShareDevices, func(v string) bool { return v == id })
	if on {
		c.ShareDevices = append(c.ShareDevices, id)
	}
}

// RememberDevice records a device seen playing. It reports whether the list
// changed enough to be worth saving (new device, renamed, or last seen more
// than an hour ago).
func (c *Config) RememberDevice(d Device) bool {
	for i, k := range c.KnownDevices {
		if k.ID != d.ID {
			continue
		}
		if k.Name == d.Name && k.Product == d.Product && d.LastSeen.Sub(k.LastSeen) < time.Hour {
			return false
		}
		c.KnownDevices[i] = d
		return true
	}
	c.KnownDevices = append(c.KnownDevices, d)
	if len(c.KnownDevices) > maxKnownDevices {
		// Drop the device seen longest ago that is not selected.
		oldest := -1
		for i, k := range c.KnownDevices {
			if slices.Contains(c.ShareDevices, k.ID) {
				continue
			}
			if oldest < 0 || k.LastSeen.Before(c.KnownDevices[oldest].LastSeen) {
				oldest = i
			}
		}
		if oldest >= 0 {
			c.KnownDevices = slices.Delete(c.KnownDevices, oldest, oldest+1)
		}
	}
	return true
}
