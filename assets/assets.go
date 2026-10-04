// Package assets embeds the tray icons.
package assets

import _ "embed"

// Icon is shown while something is playing.
//
//go:embed icon.ico
var Icon []byte

// IconIdle is shown when nothing is being shared.
//
//go:embed icon-idle.ico
var IconIdle []byte
