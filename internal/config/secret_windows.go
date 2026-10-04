//go:build windows

package config

import (
	"encoding/base64"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const dpapiPrefix = "dpapi:"

// protect encrypts the token for the current Windows user with DPAPI.
func protect(plain string) (string, error) {
	in := []byte(plain)
	inBlob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&inBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	enc := unsafe.Slice(out.Data, out.Size)
	return dpapiPrefix + base64.StdEncoding.EncodeToString(enc), nil
}

func unprotect(stored string) (string, error) {
	if !strings.HasPrefix(stored, dpapiPrefix) {
		// Plain token pasted into the file by hand.
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, dpapiPrefix))
	if err != nil || len(raw) == 0 {
		return "", err
	}
	inBlob := windows.DataBlob{Size: uint32(len(raw)), Data: &raw[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&inBlob, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
