# VDLP

A front end for the command line media downloaders, as
[Media Downloader](https://github.com/mhogomchungu/media-downloader) is, with a
native interface [MyGo](https://mygo.egoist.dev) draws itself in Go: no web page,
no Electron, and one binary for Windows, Linux and macOS.

It drives **yt-dlp, youtube-dl, gallery-dl, lux, you-get, svtplay-dl, aria2c,
wget** and **Safari Books**. Each is a program you install yourself; the app only
tells it what to fetch.

## Install

Take the release from the
[releases page](https://github.com/4imd3v/vdlp/releases/latest). VDLP is one
binary; each of these puts it where the desktop can start it. The script and
the Windows installer install for your user alone, and keep the app up to date
by itself. Neither installs a downloader: see "what it needs" below.

### Linux

Installs in `~/.local/vdlp.app`, with an entry in the applications menu, and
updates itself from then on:

```sh
curl -fsSL https://github.com/4imd3v/vdlp/releases/latest/download/install.sh | sh
```

Or the Debian package, which `apt` updates instead: it installs in `/opt`,
where the app may not write, so its own check says updates are unavailable
(the release page has it as `vdlp_<version>_amd64.deb`, or `_arm64`):

```sh
sudo apt install ./vdlp_0.2.1_amd64.deb
```

`vdlp-<version>-linux-amd64.tar.gz` and `-arm64.tar.gz` hold the binary on its
own, for anywhere else.

### macOS

The disk image, to drag VDLP into Applications, or the script, which installs
into `/Applications` when it may and `~/Applications` when it may not, and
updates itself:

```sh
curl -fsSL https://github.com/4imd3v/vdlp/releases/latest/download/install.sh | sh
```

The app is signed ad hoc, not with a Developer ID: the first time, right-click
it in Finder and choose Open.

### Windows

Installs for the current user under `%LOCALAPPDATA%\Programs\VDLP`, and
updates itself from then on:

```powershell
irm https://github.com/4imd3v/vdlp/releases/latest/download/install.ps1 | iex
```

Or run `vdlp-setup-<version>-amd64.exe` from the release (`-arm64` for ARM).
It is not code-signed, so SmartScreen warns the first time.

### What it needs

One of the downloaders, in `PATH` or at a path set in **Settings**: `yt-dlp`
for YouTube and the thousand sites it covers, and the others as their READMEs
say. `ffmpeg` is needed for the merges an engine asks of it, and for embedding
subtitles in a download. The Settings page lists each engine and says whether
it found one.

## What it does

- **New download** — a link, an engine, a preset, and extra options. "Get
  formats" asks the engine what the link offers and lists its streams, so you
  can merge the video and the audio you want.
- **Batch download** — many links at once, typed one by one, pasted, or read
  from a file with one link per line.
- **Playlist** — list what a playlist holds and download the entries you choose.
- **Subtitles** — the subtitles of a video or of every video in a playlist,
  fetched as they are and rewritten into blocks of the two lines a player
  draws, so they stay where they are put. A video download can have them
  embedded as it is saved, in the languages you name.
- **Library** — the downloads of this session, with progress, speed and ETA;
  pause, resume, start over, open the file, show it in its folder, read what the
  downloader said, and copy the link.
- **Settings** — the download folder, the file name template, how many run at
  once, a cookies file for sites that ask you to sign in, a proxy, keeping the
  computer awake, and notifications. Each engine's executable can be pointed at
  a path of your own, or left to be found in `PATH`.
- **Logs** — what the app did and what went wrong, newest last. Every download
  is recorded with the command it ran and, when it failed, the downloader's own
  last line of output; a crash is recorded with its stack. Filter by level, copy
  it, or open the file.

The engine is guessed from the link's host, as Media Downloader does, and the
preset last used with an engine is remembered.

Eight pages in a sidebar: `Cmd+1` to `Cmd+8`, and the `Go` menu, go to them.

## Run it

```sh
go tool mygo dev      # build and run, rebuilding as you edit
go tool mygo build    # package it for this machine
go test ./...         # the view and the download manager, without a window
CGO_ENABLED=1 go test -race ./...   # the queue is concurrent: run this too
```

`go tool mygo vet .` checks the stored build values and the goroutines the view
captures.

`go test -run TestRenderPages .` draws every page to a PNG with `ui.Render`, to
look at the layout without a window; `VDLP_RENDER_DIR=/tmp` says where.

## Layout

| File | What it holds |
|---|---|
| `main.go` | the window, the menus, and quitting |
| `app.go` | the app's state, the view, and the pages' shared parts |
| `pages.go` | the eight pages |
| `engines.go` | the downloaders, their presets, and the guess from a link |
| `downloader.go` | the download queue, its progress, and the downloaders' output |
| `info.go` | what an engine's `-J` says about a link |
| `settings.go` | the settings, kept as JSON in the app's data directory |
| `logs.go` | the log: a file on disk, and the tail the Logs page shows |
| `icons.go` | the icons, drawn from SVG paths |
| `subs.go` | the subtitles: read, rewritten into blocks, and muxed in |

`logs_test.go`, `manager_test.go`, `main_test.go`, `settings_test.go`,
`subs_test.go` and `zrender_test.go` hold the tests: the view driven through
`ui.NewTester`, real downloads run against a shell script, and the pages drawn
to PNGs.

## Releasing and updates

Bump `version` in `mygo.json`, write its section in `CHANGELOG.md` — that
section is the release notes the app shows — commit, and push the tag of that
version:

```sh
git tag v0.2.1
git push origin v0.2.1
```

`.github/workflows/release.yml` runs the tests, builds Windows, Linux and macOS
on a runner of each with `mygo build -upload`, and uploads the disk image, the
installers, the `.deb`, the `.tar.gz` and the signed update files to a draft
release. The tag must be the version in `mygo.json`: the workflow checks it
before it builds anything. Publishing the draft is what makes installed apps
see the version — `-upload` stops before building when the signing key is
missing, so a release without one is never published.

An installed VDLP checks for a new version ten seconds after it starts, then
once a day, and shows what changed with an **Install Update** / **Remind Me
Later** / **Skip This Version** window; **Check for Updates…** in the Help menu
asks at once (`mygo/update-keys` holds the details). It downloads only what
changed when the release has a delta for the version it runs, checks the
signature with the public key built into it, replaces itself, and relaunches.

- The update key is `~/.config/mygo/update-keys/mygo-update.key` (`go tool
  mygo keygen`). Its public key is `updates.publicKey` in `mygo.json`, and the
  secret is the repository's `MYGO_UPDATER_PRIVATE_KEY` secret. Keep it in a
  password manager as well: installed apps refuse anything signed with another
  key, so losing it strands them on the version they have.
- Installations that can update themselves are the macOS app, the Windows
  installer's per-user install, and the Linux archive `install.sh` puts in
  `~/.local/vdlp.app`. The `.deb` is installed in `/opt`, so Debian updates it
  and the app does not.
- The update archives are signed with the update key, but the binaries are not
  code-signed: the macOS app is signed ad hoc, so Gatekeeper asks the user to
  right-click and Open it the first time, and Windows warns SmartScreen.
  Signing needs a Developer ID certificate and notarization, and a Windows
  certificate, as MyGo's distribution docs describe.
- An update relaunches the app, and the queue is given up as it quits: a
  download running when the update installs does not survive it.

## Before you use it

Download only what the law where you are lets you download, and what the site's
terms allow. The app checks neither.
