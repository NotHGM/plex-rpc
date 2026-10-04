// Package tray is the system tray UI.
package tray

import (
	"context"
	"log"
	"path/filepath"
	"sync/atomic"

	"fyne.io/systray"

	"github.com/NotHGM/plex-rpc/assets"
	"github.com/NotHGM/plex-rpc/internal/app"
	"github.com/NotHGM/plex-rpc/internal/config"
	"github.com/NotHGM/plex-rpc/internal/sysutil"
)

// Run shows the tray icon and blocks until the user quits. stop is called on
// quit so the caller can shut the app loop down.
func Run(ctx context.Context, a *app.App, version string, stop func()) {
	systray.Run(func() { onReady(ctx, a, version, stop) }, func() {})
}

func onReady(ctx context.Context, a *app.App, version string, stop func()) {
	systray.SetIcon(assets.IconIdle)
	systray.SetTitle("Plex RPC")
	systray.SetTooltip("Plex RPC")

	header := systray.AddMenuItem("Plex RPC "+version, "")
	header.Disable()
	status := systray.AddMenuItem("Starting…", "")
	status.Disable()
	server := systray.AddMenuItem("", "")
	server.Disable()
	server.Hide()
	systray.AddSeparator()

	cfg := a.Store().Get()
	pause := systray.AddMenuItemCheckbox("Pause presence", "Stop sharing without quitting", cfg.Disabled)
	anyDevice := systray.AddMenuItemCheckbox("Show playback from all my devices",
		"Off: only the Plex app on this PC. On: TVs, phones and other players too", cfg.PlayerFilter == config.PlayerAny)

	kinds := systray.AddMenuItem("Share", "")
	movies := kinds.AddSubMenuItemCheckbox("Movies", "", cfg.ShowMovies)
	episodes := kinds.AddSubMenuItemCheckbox("TV shows", "", cfg.ShowEpisodes)
	music := kinds.AddSubMenuItemCheckbox("Music", "", cfg.ShowMusic)
	buttons := kinds.AddSubMenuItemCheckbox("IMDb / TMDB buttons", "", cfg.ShowButtons)
	upload := kinds.AddSubMenuItemCheckbox("Upload artwork for personal media",
		"Uploads posters that have no public URL to litterbox.catbox.moe for 72 hours", cfg.UploadArtwork)

	autostart := systray.AddMenuItemCheckbox("Start with Windows", "", sysutil.AutostartEnabled())
	systray.AddSeparator()
	account := systray.AddMenuItem("Sign in to Plex…", "")
	settings := systray.AddMenuItem("Open settings folder", "config.json and the log file")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Quit", "")

	var signedIn atomic.Bool
	a.OnStatus(func(st app.Status) {
		status.SetTitle(st.Text)
		if st.Server != "" {
			server.SetTitle("Server: " + st.Server)
			server.Show()
		} else {
			server.Hide()
		}
		tip := "Plex RPC · " + st.Text
		if len([]rune(tip)) > 120 {
			tip = string([]rune(tip)[:119]) + "…"
		}
		systray.SetTooltip(tip)
		if st.State == app.StatePlaying || st.State == app.StatePaused {
			systray.SetIcon(assets.Icon)
		} else {
			systray.SetIcon(assets.IconIdle)
		}
		in := st.Account != ""
		signedIn.Store(in)
		switch {
		case st.State == app.StateSigningIn:
			account.SetTitle("Restart sign-in…")
		case in:
			account.SetTitle("Sign out (" + st.Account + ")")
		default:
			account.SetTitle("Sign in to Plex…")
		}
	})

	toggle := func(item *systray.MenuItem, apply func(c *config.Config, on bool)) {
		on := !item.Checked()
		if on {
			item.Check()
		} else {
			item.Uncheck()
		}
		if err := a.Store().Update(func(c *config.Config) { apply(c, on) }); err != nil {
			log.Printf("settings: %v", err)
		}
		a.Wake()
	}

	go func() {
		for {
			select {
			case <-pause.ClickedCh:
				toggle(pause, func(c *config.Config, on bool) { c.Disabled = on })
			case <-anyDevice.ClickedCh:
				toggle(anyDevice, func(c *config.Config, on bool) {
					c.PlayerFilter = config.PlayerThisPC
					if on {
						c.PlayerFilter = config.PlayerAny
					}
				})
			case <-movies.ClickedCh:
				toggle(movies, func(c *config.Config, on bool) { c.ShowMovies = on })
			case <-episodes.ClickedCh:
				toggle(episodes, func(c *config.Config, on bool) { c.ShowEpisodes = on })
			case <-music.ClickedCh:
				toggle(music, func(c *config.Config, on bool) { c.ShowMusic = on })
			case <-buttons.ClickedCh:
				toggle(buttons, func(c *config.Config, on bool) { c.ShowButtons = on })
			case <-upload.ClickedCh:
				toggle(upload, func(c *config.Config, on bool) { c.UploadArtwork = on })
			case <-autostart.ClickedCh:
				on := !autostart.Checked()
				if err := sysutil.SetAutostart(on); err != nil {
					log.Printf("autostart: %v", err)
					continue
				}
				if on {
					autostart.Check()
				} else {
					autostart.Uncheck()
				}
			case <-account.ClickedCh:
				if signedIn.Load() {
					a.SignOut()
				} else {
					a.SignIn(ctx)
				}
			case <-settings.ClickedCh:
				_ = sysutil.OpenURL(filepath.Dir(a.Store().Path()))
			case <-quit.ClickedCh:
				stop()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Quit closes the tray (call after the app loop has finished).
func Quit() { systray.Quit() }
