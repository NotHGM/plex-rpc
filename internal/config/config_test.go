package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenDefaultsAndPersist(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if !strings.HasPrefix(c.ClientIdentifier, "plex-rpc-") || !c.ShareThisPC || c.ShareAllDevices || c.ClearAfterPauseMinutes != 5 {
		t.Fatalf("defaults %+v", c)
	}
	if err := s.SetToken("secret"); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tok, _ := s2.Token(); tok != "secret" {
		t.Errorf("token %q", tok)
	}
	if s2.Get().ClientIdentifier != c.ClientIdentifier {
		t.Error("client identifier should be stable")
	}
}

func TestReloadIfChanged(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := s.ReloadIfChanged(); r {
		t.Error("no change expected")
	}
	b, _ := os.ReadFile(s.Path())
	b = []byte(strings.Replace(string(b), `"share_all_devices": false`, `"share_all_devices": true`, 1))
	if err := os.WriteFile(s.Path(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(s.Path(), future, future)
	if r, err := s.ReloadIfChanged(); !r || err != nil {
		t.Fatalf("reload = %v, %v", r, err)
	}
	if !s.Get().ShareAllDevices {
		t.Error("edit not picked up")
	}
}

func TestMigratePlayerFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/config.json", []byte(`{"player_filter":"any"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c := s.Get(); !c.ShareAllDevices || !c.ShareThisPC || c.PlayerFilter != "" {
		t.Errorf("migrated %+v", c)
	}
}

func TestSharedAccounts(t *testing.T) {
	c := Config{AccountID: 1}
	if got := c.SharedAccounts(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("default %v", got)
	}
	c.SetAccountShared(7, true)
	if got := c.SharedAccounts(); len(got) != 2 {
		t.Fatalf("after add %v", got)
	}
	c.SetAccountShared(1, false)
	c.SetAccountShared(7, false)
	if got := c.SharedAccounts(); len(got) != 0 {
		t.Fatalf("unticking everyone should share nobody, got %v", got)
	}
}

func TestRememberDevice(t *testing.T) {
	var c Config
	now := time.Now()
	if !c.RememberDevice(Device{ID: "ps5", Name: "PS5", LastSeen: now}) {
		t.Fatal("new device not saved")
	}
	if c.RememberDevice(Device{ID: "ps5", Name: "PS5", LastSeen: now.Add(time.Minute)}) {
		t.Fatal("unchanged device should not force a save")
	}
	if !c.RememberDevice(Device{ID: "ps5", Name: "Living room PS5", LastSeen: now}) {
		t.Fatal("rename should be saved")
	}
	c.SetDeviceShared("ps5", true)
	for i := 0; i < maxKnownDevices+5; i++ {
		c.RememberDevice(Device{ID: string(rune('a' + i)), LastSeen: now.Add(time.Duration(i) * time.Second)})
	}
	if len(c.KnownDevices) != maxKnownDevices {
		t.Fatalf("kept %d devices", len(c.KnownDevices))
	}
	found := false
	for _, d := range c.KnownDevices {
		found = found || d.ID == "ps5"
	}
	if !found {
		t.Fatal("selected device was evicted")
	}
}
