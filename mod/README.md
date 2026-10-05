# Plex RPC — in-app mod

An alternative to the standalone Plex RPC app, for people who would rather
not run a separate program. Instead of its own tray app, the engine is
loaded **inside the Plex desktop app**: when Plex starts, Rich Presence
starts; when Plex closes, it stops. Nothing else runs.

It is two files that sit next to Plex:

| File | What it is |
| --- | --- |
| `version.dll` | A transparent proxy for the Windows `version.dll`. Plex loads it from its own folder; it forwards every real call on to the genuine Windows library, and also starts the engine. |
| `plexrpc_core.dll` | The Plex RPC engine (the same logic as the standalone app), loaded by the proxy. |

It changes nothing about how Plex works — the proxy only adds to the real
`version.dll`, it never replaces Windows' copy. Still, read the risks below
and know how to remove it. x64 Plex only for now.

## Install

1. Quit Plex completely, including from the system tray.
2. Find your Plex program folder. Right-click the Plex shortcut →
   **Open file location**. It is usually `C:\Program Files\Plex\Plex\`
   (the folder that contains `Plex.exe`).
3. Copy **both** `version.dll` and `plexrpc_core.dll` into that folder.
4. Start Plex. The first time, a browser window opens to sign in to Plex —
   approve **Plex RPC**. After that it is automatic.

Play something and check your Discord profile.

## Uninstall

Quit Plex, then delete `version.dll` and `plexrpc_core.dll` from the Plex
folder. That is all.

## Settings and logs

The mod shares the standalone app's settings file,
`%APPDATA%\plex-rpc\config.json`, so all the same options apply (devices,
accounts, media types, artwork, and so on). Its log is
`%APPDATA%\plex-rpc\plex-rpc-mod.log`.

Use the mod **or** the standalone app, not both at once — they would both
set your Discord status and fight over it.

## Risks (please read)

- **Antivirus.** A `version.dll` placed next to a program is a pattern some
  antivirus tools treat as suspicious, because malware abuses the same
  technique. These files are not signed, so Windows Defender may warn about
  them or quarantine them. If Plex RPC stops working, check your antivirus
  quarantine.
- **Plex updates.** An update may delete these files (just re-copy them) or,
  less often, change Plex so the mod needs rebuilding.
- **If Plex won't start** after installing: quit Plex and delete the two
  files. Plex returns to normal immediately — the proxy only adds to the
  real `version.dll`, it does not replace Windows' copy.

## Build from source

```sh
DISCORD_CLIENT_ID=<your app id> mod/build.sh v0.1.0
```

Needs Go and an llvm-mingw (or mingw-w64) cross toolchain on `PATH`. The
proxy source is `mod/proxy/proxy.c`; the engine wrapper is `mod/core`.
