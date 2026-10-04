// Package tray is the system tray UI.
package tray

import (
	"context"
	"log"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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

	devicesMenu := systray.AddMenuItem("Devices", "Which players to share")
	allDevices := devicesMenu.AddSubMenuItemCheckbox("All devices", "Share playback from every device", cfg.ShareAllDevices)
	devicesMenu.AddSeparator()
	thisPC := devicesMenu.AddSubMenuItemCheckbox("This PC", "The Plex app on this computer", cfg.ShareThisPC)
	noDevices := devicesMenu.AddSubMenuItem("Other devices appear here once they play something", "")
	noDevices.Disable()

	accountsMenu := systray.AddMenuItem("Accounts", "Whose playback counts as yours")
	noAccounts := accountsMenu.AddSubMenuItem("Sign in to see your Plex Home users", "")
	noAccounts.Disable()

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

	var refresh func()
	devices := newList(ctx, devicesMenu, func(id string, on bool) {
		update(a, func(c *config.Config) { c.SetDeviceShared(id, on) })
		refresh()
	})
	accounts := newList(ctx, accountsMenu, func(id string, on bool) {
		n, _ := strconv.ParseInt(id, 10, 64)
		update(a, func(c *config.Config) { c.SetAccountShared(n, on) })
		refresh()
	})
	refresh = func() {
		cfg := a.Store().Get()
		setChecked(allDevices, cfg.ShareAllDevices)
		setChecked(thisPC, cfg.ShareThisPC)
		if cfg.ShareAllDevices {
			thisPC.Disable()
		} else {
			thisPC.Enable()
		}
		devices.sync(deviceRows(cfg))
		if devices.len() > 0 {
			noDevices.Hide()
		} else {
			noDevices.Show()
		}
		accounts.sync(accountRows(cfg))
		if accounts.len() > 0 {
			noAccounts.Hide()
		} else {
			noAccounts.Show()
		}
	}
	refresh()
	a.OnKnownChange(refresh)

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
		setChecked(item, on)
		update(a, func(c *config.Config) { apply(c, on) })
	}

	go func() {
		for {
			select {
			case <-pause.ClickedCh:
				toggle(pause, func(c *config.Config, on bool) { c.Disabled = on })
			case <-allDevices.ClickedCh:
				toggle(allDevices, func(c *config.Config, on bool) { c.ShareAllDevices = on })
				refresh()
			case <-thisPC.ClickedCh:
				toggle(thisPC, func(c *config.Config, on bool) { c.ShareThisPC = on })
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
				setChecked(autostart, on)
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

func update(a *app.App, fn func(c *config.Config)) {
	if err := a.Store().Update(fn); err != nil {
		log.Printf("settings: %v", err)
	}
	a.Wake()
}

func deviceRows(cfg config.Config) []row {
	devs := slices.Clone(cfg.KnownDevices)
	slices.SortFunc(devs, func(x, y config.Device) int { return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name)) })
	rows := make([]row, 0, len(devs))
	for _, d := range devs {
		title := d.Name
		if d.Product != "" && !strings.EqualFold(d.Product, d.Name) {
			title += " (" + d.Product + ")"
		}
		rows = append(rows, row{
			ID:       d.ID,
			Title:    title,
			Tooltip:  "Last played " + d.LastSeen.Local().Format("2 Jan 2006 15:04"),
			Checked:  slices.Contains(cfg.ShareDevices, d.ID),
			Disabled: cfg.ShareAllDevices,
		})
	}
	return rows
}

func accountRows(cfg config.Config) []row {
	if cfg.AccountID == 0 && cfg.AccountName == "" {
		return nil
	}
	known := cfg.KnownAccounts
	if len(known) == 0 {
		known = []config.Account{{ID: cfg.AccountID, Title: cfg.AccountTitle, Admin: true}}
	}
	shared := cfg.SharedAccounts()
	rows := make([]row, 0, len(known))
	for _, k := range known {
		title := k.Title
		if title == "" {
			title = cfg.AccountName
		}
		if k.ID == cfg.AccountID {
			title += " (you)"
		}
		rows = append(rows, row{ID: strconv.FormatInt(k.ID, 10), Title: title, Checked: slices.Contains(shared, k.ID)})
	}
	return rows
}

// Quit closes the tray (call after the app loop has finished).
func Quit() { systray.Quit() }
