<div align="center">
  <img src="assets/icon.png" width="96" alt="Plex RPC icon">
  <h1>Plex RPC</h1>
  <p>Discord Rich Presence for the Plex desktop app on Windows.</p>
  <p>
    <a href="https://github.com/NotHGM/plex-rpc/releases/latest"><b>Download</b></a> ·
    <a href="#install">Install</a> ·
    <a href="#settings">Settings</a> ·
    <a href="#troubleshooting">Troubleshooting</a>
  </p>
</div>

---

Plex RPC sits in your system tray and shows what you're playing in Plex on your Discord profile: the show, episode or movie, its poster, and a live progress bar. Music shows up as **Listening to**, video as **Watching**.

```
 ┌──────────────────────────────────────────────┐
 │  Watching Plex                               │
 │  ┌──────┐  Breaking Bad                      │
 │  │poster│  S05E14 · Ozymandias               │
 │  │    ▶ │  ━━━━━━━━━━━━━━━━━━━──────  12:41  │
 │  └──────┘                                    │
 │  [ IMDb ]  [ TMDB ]                          │
 └──────────────────────────────────────────────┘
```

## Features

- **Works with the Plex desktop app.** Plex for Windows and Plex HTPC, no browser extension, no Plex Pass.
- **Real posters.** Artwork comes from Plex's public metadata service, so your server is never exposed. Music falls back to iTunes cover art.
- **Progress bar** that stays in sync when you pause, seek or skip.
- **You choose what counts.** By default only your account on this PC is shared. Tick your PS5, TV or phone under **Devices**, and your Plex Home users under **Accounts**. Friends streaming from your server are never shown.
- **Sign in with Plex** in your browser. No token copying, no server address to type.
- **Tray app** with a pause switch, device and account pickers, per-media toggles and *Start with Windows*.
- **Two ways to run it** — a small tray app, or an [in-app mod](#option-2--in-app-mod) that lives inside Plex so nothing extra runs.
- No installer and no runtime to install.

## Install

There are two ways to run Plex RPC. Both do the same thing — pick whichever you prefer.

| | **App** (recommended) | **In-app mod** |
| --- | --- | --- |
| What it is | A small program in your system tray | Two files dropped next to Plex |
| Extra program running | Yes (a tray icon) | No — it runs inside Plex |
| Install | Run the `.exe` | Copy two files into the Plex folder |
| Start / stop | With Windows, or manually | Automatically with Plex |
| Best for | Most people | People who don't want a separate program |

Whichever you pick, the **Discord desktop app must be running** — the web version can't show Rich Presence — and you sign in to Plex once in your browser.

### Option 1 — App (recommended)

1. Download `plex-rpc-windows-amd64.exe` from the [latest release](https://github.com/NotHGM/plex-rpc/releases/latest). Use the `arm64` build on Snapdragon PCs.
2. Put it somewhere permanent, for example `%LOCALAPPDATA%\Programs\Plex RPC\`, and run it.
3. Your browser opens the Plex sign-in page. Approve **Plex RPC**.
4. Right-click the tray icon and tick **Start with Windows**.

Play something in Plex and check your Discord profile.

> Windows SmartScreen may warn about an unrecognised app the first time because the exe isn't code-signed. Click **More info → Run anyway**. You can also [build it yourself](#building-from-source).

### Option 2 — In-app mod

Runs inside the Plex desktop app, so there's no separate program. It's two files that sit next to `Plex.exe`; Plex loads them when it starts and they stop when Plex closes.

1. Download `plex-rpc-mod-windows-amd64.zip` from the [latest release](https://github.com/NotHGM/plex-rpc/releases/latest) and unzip it.
2. Quit Plex completely, including from the system tray.
3. Open your Plex program folder (right-click the Plex shortcut → **Open file location**, usually `C:\Program Files\Plex\Plex\`).
4. Copy **both** `version.dll` and `plexrpc_core.dll` into that folder.
5. Start Plex and approve the one-time Plex sign-in in your browser.

To remove it, quit Plex and delete those two files. Full details, including antivirus notes, are in [`mod/README.md`](mod/README.md). Use the app **or** the mod, not both at once. The mod is x64 only for now.

## How it works

The Plex desktop app has no local API. It does, however, report playback to your Plex Media Server like every Plex client. Plex RPC asks your server for the current sessions every couple of seconds, keeps the ones that belong to your account and come from this computer, and sends them to Discord over its local IPC pipe.

```
 Plex for Windows ──timeline──▶ Plex Media Server ◀──/status/sessions── Plex RPC ──IPC──▶ Discord
```

Because of this, Plex RPC needs to be signed in to the account that **owns** the server. Only the owner can read the session list.

### Devices and accounts

The tray has two pickers:

- **Devices**: *This PC* is on by default. Any other device that someone in your Plex Home plays on, such as a PS5, TV or phone, is added to this list the first time it plays something, so you can tick it. *All devices* shares everything.
- **Accounts**: lists your main account and your Plex Home users, including managed users. Tick the ones whose playback should count as yours.

Something is shown only if **both** match: a ticked account, playing on a ticked device. Your PC still has to be on with Plex RPC and Discord running, even when the playback is on another device.

## Settings

The tray menu covers the common options. Everything lives in `%APPDATA%\plex-rpc\config.json` (tray → *Open settings folder*). Edits are picked up automatically.

| Key | Default | Description |
| --- | --- | --- |
| `share_this_pc` | `true` | Share playback from the Plex app on this computer. |
| `share_devices` | `[]` | Machine IDs of other devices to share. Easiest to set from tray → *Devices*. |
| `share_all_devices` | `false` | Share every device, including ones not in the list yet. |
| `share_accounts` | `null` | plex.tv account IDs whose playback counts as yours. `null` means only the signed-in account. Easiest to set from tray → *Accounts*. |
| `clear_after_pause_minutes` | `5` | Hide the presence after being paused this long. `0` hides it on pause, `-1` never hides it. |
| `show_movies` / `show_episodes` / `show_music` | `true` | Choose what gets shared. |
| `show_buttons` | `true` | Add IMDb / TMDB buttons. Other people see them, you don't. |
| `upload_artwork` | `false` | For home videos or anything Plex can't match, upload the poster to [litterbox.catbox.moe](https://litterbox.catbox.moe) (deleted after 72 hours) so Discord can show it. |
| `server_url` | `""` | Skip discovery and use this server, e.g. `http://192.168.1.10:32400`. |
| `poll_interval_seconds` | `2` | How often the server is checked. |
| `discord_client_id` | `""` | Use your own Discord application (see below). Empty uses the built-in one. |
| `disabled` | `false` | Same as *Pause presence* in the tray. |

Your Plex token is stored in the same file, encrypted with Windows DPAPI so only your Windows account can read it.

## Using your own Discord application

The activity title (**Watching *Plex***) is the name of the Discord application. Release builds include one. If you build from source, or want a different name or images:

1. Open the [Discord Developer Portal](https://discord.com/developers/applications), click **New Application** and name it `Plex`.
2. Copy the **Application ID** into `discord_client_id` in `config.json`.
3. Under **Rich Presence → Art Assets**, upload the three images from [`assets/discord`](assets/discord) with these exact names:

   | Name | File |
   | --- | --- |
   | `plex` | `plex.png`: shown when no poster is found |
   | `play` | `play.png`: small badge while playing |
   | `pause` | `pause.png`: small badge while paused |

New assets can take a few minutes to appear in Discord.

## Troubleshooting

Start with the log: tray → *Open settings folder* → `plex-rpc.log`.

**Nothing shows up while I'm playing.**
Look for a line starting with `ignoring` in the log. It lists the user, player name and address the server reported.
- If the player is a different device, tick it under **Devices**, or tick **All devices**.
- If it's this PC but wasn't recognised (some Docker or VPN setups hide the real address), also enable that option. Your phone and TV will show up too.
- If the user is one of your Plex Home users, tick them under **Accounts**. Otherwise sign in with the account that owns the server.

**It still shows a movie after I closed Plex.**
If you close the app mid-playback, it never tells the server it stopped, so the server keeps listing the session as playing for a while. Plex RPC notices when the position stops moving, or when no Plex app is running on this PC, and clears the status within about 30 seconds.

**The tray says "Discord isn't running".**
Start the Discord desktop app. Plex RPC reconnects on its own. If Discord runs as administrator, Plex RPC must too, or neither can see the other.

**"Can't reach your Plex server".**
Check the server is up, or set `server_url` to its LAN address.

**My profile shows the Plex logo instead of the poster.**
The item isn't matched to Plex's online metadata (personal media, or a library using a local agent). Turn on **Upload artwork for personal media** if you're fine with the poster being uploaded temporarily.

**I can't see the buttons.**
Discord hides presence buttons from yourself. Ask a friend or check from another account.

## Privacy

Plex RPC only talks to:

- **plex.tv**, to sign in and find your servers
- **your Plex Media Server**, to read sessions
- **metadata.provider.plex.tv**, to look up public poster URLs
- **itunes.apple.com**, to search album art for music
- **litterbox.catbox.moe**, only if you enable artwork uploads
- **the Discord app on your PC**, over a local pipe

There's no telemetry. Sign out from the tray to delete the stored token, and remove the device under *Authorized Devices* on plex.tv to revoke it.

## Building from source

Requires [Go](https://go.dev/dl/) 1.26 or newer.

```sh
git clone https://github.com/NotHGM/plex-rpc
cd plex-rpc
DISCORD_CLIENT_ID=<your app id> scripts/build.sh
```

This writes `plex-rpc-windows-amd64.exe` and `plex-rpc-windows-arm64.exe` to `dist/`, with the icon, version info and your Discord application ID built in. Without the script, on any OS:

```sh
go generate ./cmd/plex-rpc        # embed icon + version info (optional)
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui -s -w -X main.defaultClientID=<your app id>" -o plex-rpc.exe ./cmd/plex-rpc
```

On Windows PowerShell, set the variables first with `$env:GOOS="windows"; $env:GOARCH="amd64"`.

- `go test ./...` runs the test suite, which includes an end-to-end test against a fake Plex server and a fake Discord client.
- `go run ./cmd/plex-rpc -headless` runs without the tray and logs to the console. This works on Linux too.
- `go run ./tools/genicon` regenerates every icon and Discord asset from code.

### Building the in-app mod

The mod shares the same engine, built as a DLL. It needs an [llvm-mingw](https://github.com/mstorsjo/llvm-mingw) (or mingw-w64) cross toolchain on `PATH` in addition to Go:

```sh
DISCORD_CLIENT_ID=<your app id> mod/build.sh v0.3.0
```

This writes `version.dll` and `plexrpc_core.dll` to `mod/dist/`. See [`mod/README.md`](mod/README.md) for how it works.

### Project layout

```
cmd/plex-rpc        entry point, single-instance guard, logging
internal/app        polling loop, sign-in, status
internal/plex       plex.tv + Plex Media Server API
internal/discord    Discord IPC client (handshake, SET_ACTIVITY)
internal/presence   session filtering, activity text, change detection
internal/artwork    public poster lookup and caching
internal/tray       system tray menu
internal/sysutil    autostart, browser, local IPs
mod/core            engine built as a DLL (plexrpc_core.dll)
mod/proxy           version.dll proxy that loads the engine inside Plex
```

## License

[MIT](LICENSE). Plex RPC isn't affiliated with or endorsed by Plex, Inc. or Discord Inc.
