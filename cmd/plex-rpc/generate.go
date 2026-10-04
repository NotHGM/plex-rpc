package main

// Embeds the icon, version info and a DPI-aware manifest into the Windows exe.
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64,arm64 --manifest gui --icon ../../assets/icon.ico --product-name "Plex RPC" --file-description "Discord Rich Presence for Plex" --original-filename plex-rpc.exe --copyright "NotHGM" --product-version git-tag --file-version git-tag
