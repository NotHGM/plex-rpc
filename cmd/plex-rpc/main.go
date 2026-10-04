// Command plex-rpc shows what you are playing in the Plex desktop app as
// Discord Rich Presence.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/NotHGM/plex-rpc/internal/app"
	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/sysutil"
	"github.com/NotHGM/plex-rpc/internal/tray"
)

// Set at build time with -ldflags "-X main.version=... -X main.defaultClientID=...".
var (
	version         = "dev"
	defaultClientID = ""
)

func main() {
	headless := flag.Bool("headless", false, "run without a tray icon and log to stdout")
	signIn := flag.Bool("signin", false, "start the Plex sign-in flow on launch")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("plex-rpc", version)
		return
	}
	if !sysutil.SingleInstance() {
		sysutil.Alert("Plex RPC", "Plex RPC is already running. Look for its icon in the system tray.")
		return
	}

	dir, err := config.Dir()
	if err != nil {
		fatal(err)
	}
	store, err := config.Open(dir)
	if err != nil {
		fatal(err)
	}
	logFile := setupLog(dir, *headless)
	if logFile != nil {
		defer logFile.Close()
	}
	log.Printf("plex-rpc %s starting, settings in %s", version, dir)
	sysutil.RefreshAutostart()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	a := app.New(store, version, defaultClientID, dir)
	if tok, _ := store.Token(); tok == "" || *signIn {
		a.SignIn(ctx)
	}

	if *headless {
		a.OnStatus(func(st app.Status) { log.Printf("status: %s", st.Text) })
		a.Run(ctx)
		return
	}

	done := make(chan struct{})
	go func() {
		a.Run(ctx)
		close(done)
		tray.Quit()
	}()
	tray.Run(ctx, a, version, cancel)
	cancel()
	<-done
	log.Printf("bye")
}

// setupLog writes to plex-rpc.log, keeping one previous file.
func setupLog(dir string, stdout bool) *os.File {
	path := filepath.Join(dir, "plex-rpc.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 2<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	if stdout {
		log.SetOutput(io.MultiWriter(os.Stdout, f))
	} else {
		log.SetOutput(f)
	}
	log.SetFlags(log.LstdFlags)
	return f
}

func fatal(err error) {
	sysutil.Alert("Plex RPC", err.Error())
	os.Exit(1)
}
