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
	if !strings.HasPrefix(c.ClientIdentifier, "plex-rpc-") || c.PlayerFilter != PlayerThisPC || c.ClearAfterPauseMinutes != 5 {
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
	b = []byte(strings.Replace(string(b), `"player_filter": "this_pc"`, `"player_filter": "any"`, 1))
	if err := os.WriteFile(s.Path(), b, 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	_ = os.Chtimes(s.Path(), future, future)
	if r, err := s.ReloadIfChanged(); !r || err != nil {
		t.Fatalf("reload = %v, %v", r, err)
	}
	if s.Get().PlayerFilter != PlayerAny {
		t.Error("edit not picked up")
	}
}
