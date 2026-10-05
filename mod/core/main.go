//go:build windows

// Command plexrpc-core builds the Plex RPC engine as a Windows DLL
// (plexrpc_core.dll) that the Plex proxy loads inside the Plex process.
//
// It exports two C functions:
//
//	PlexRpcStart()  starts the engine on a background goroutine (returns at once)
//	PlexRpcStop()   stops it and clears the Discord presence
//
// Everything else is the same engine the standalone tray app uses.
package main

import "C"

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/NotHGM/plex-rpc/internal/app"
	"github.com/NotHGM/plex-rpc/internal/config"
)

// Set with -ldflags "-X main.version=... -X main.defaultClientID=...".
var (
	version         = "mod-dev"
	defaultClientID = ""
)

var (
	mu      sync.Mutex
	cancel  context.CancelFunc
	logFile *os.File
)

//export PlexRpcStart
func PlexRpcStart() {
	mu.Lock()
	defer mu.Unlock()
	if cancel != nil {
		return // already running
	}
	dir, err := config.Dir()
	if err != nil {
		return
	}
	openLog(dir)
	log.Printf("plex-rpc mod %s starting inside host process (pid %d)", version, os.Getpid())

	store, err := config.Open(dir)
	if err != nil {
		log.Printf("config: %v", err)
		return
	}
	ctx, c := context.WithCancel(context.Background())
	cancel = c
	a := app.New(store, version+" (mod)", defaultClientID, dir)
	if tok, _ := store.Token(); tok == "" {
		a.SignIn(ctx)
	}
	go a.Run(ctx)
}

//export PlexRpcStop
func PlexRpcStop() {
	mu.Lock()
	defer mu.Unlock()
	if cancel != nil {
		log.Printf("plex-rpc mod stopping")
		cancel()
		cancel = nil
	}
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}

func openLog(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	path := filepath.Join(dir, "plex-rpc-mod.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 2<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	logFile = f
	log.SetOutput(io.Writer(f))
	log.SetFlags(log.LstdFlags)
}

func main() {}
