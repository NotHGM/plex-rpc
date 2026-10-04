//go:build !windows

package config

// On non-Windows builds (development only) the token is stored as-is; the
// file is created with 0600 permissions.
func protect(plain string) (string, error)    { return plain, nil }
func unprotect(stored string) (string, error) { return stored, nil }
