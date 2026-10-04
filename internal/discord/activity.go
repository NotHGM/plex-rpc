package discord

// Activity types accepted over RPC.
const (
	TypePlaying   = 0
	TypeListening = 2
	TypeWatching  = 3
)

// StatusDisplayType selects which field Discord shows in the member list
// ("Watching <x>").
const (
	DisplayName    = 0
	DisplayState   = 1
	DisplayDetails = 2
)

// Activity is a Rich Presence payload.
type Activity struct {
	Type              int         `json:"type"`
	StatusDisplayType int         `json:"status_display_type"`
	Details           string      `json:"details,omitempty"`
	State             string      `json:"state,omitempty"`
	Timestamps        *Timestamps `json:"timestamps,omitempty"`
	Assets            *Assets     `json:"assets,omitempty"`
	Buttons           []Button    `json:"buttons,omitempty"`
}

// Timestamps are Unix milliseconds. With both set Discord draws a progress bar.
type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

// Assets are images: either an asset key uploaded to the Discord app or an
// https URL.
type Assets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

// Button is a link shown under the presence (visible to others, not to you).
type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
