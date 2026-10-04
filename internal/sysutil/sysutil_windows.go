//go:build windows

package sysutil

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "PlexRPC"
)

// OpenURL opens a link or folder with the default handler.
//
// ShellExecute is used rather than a helper process: a hidden helper passes
// SW_HIDE on to Explorer, which then opens the folder invisibly.
func OpenURL(target string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// AutostartEnabled reports whether plex-rpc starts with Windows.
func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}

// SetAutostart adds or removes plex-rpc from the user's startup programs.
func SetAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, `"`+exe+`"`)
}

// RefreshAutostart rewrites the startup entry when the exe has moved.
func RefreshAutostart() {
	if AutostartEnabled() {
		_ = SetAutostart(true)
	}
}

// SingleInstance returns false if another copy is already running.
func SingleInstance() bool {
	name, _ := windows.UTF16PtrFromString(`Local\PlexRPC-SingleInstance`)
	_, err := windows.CreateMutex(nil, false, name)
	return !errors.Is(err, windows.ERROR_ALREADY_EXISTS)
}

// Alert shows a message box.
func Alert(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, m, t, windows.MB_OK|windows.MB_ICONINFORMATION)
}
