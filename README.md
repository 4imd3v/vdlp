# VDLP

A front end for the command line media downloaders, as
[Media Downloader](https://github.com/mhogomchungu/media-downloader) is, with a
native interface [MyGo](https://mygo.egoist.dev) draws itself in Go: no web page,
no Electron, and one binary for Windows, Linux and macOS.

It drives **yt-dlp, youtube-dl, gallery-dl, lux, you-get, svtplay-dl, aria2c,
wget** and **Safari Books**. Each is a program you install yourself; the app only
tells it what to fetch.

## What it does

- **New download** — a link, an engine, a preset, and extra options. "Get
  formats" asks the engine what the link offers and lists its streams, so you
  can merge the video and the audio you want.
- **Batch download** — many links at once, typed one by one, pasted, or read
  from a file with one link per line.
- **Playlist** — list what a playlist holds and download the entries you choose.
- **Library** — the downloads of this session, with progress, speed and ETA;
  pause, resume, start over, open the file, show it in its folder, read what the
  downloader said, and copy the link.
- **Settings** — the download folder, the file name template, how many run at
  once, a cookies file for sites that ask you to sign in, a proxy, keeping the
  computer awake, and notifications. Each engine's executable can be pointed at
  a path of your own, or left to be found in `PATH`.

The engine is guessed from the link's host, as Media Downloader does, and the
preset last used with an engine is remembered.

## Run it

```sh
go tool mygo dev      # build and run, rebuilding as you edit
go tool mygo build    # package it for this machine
go test ./...         # the view and the download manager, without a window
```

`go tool mygo vet .` checks the stored build values and the goroutines the view
captures.

## Layout

| File | What it holds |
|---|---|
| `main.go` | the window, the menus, and quitting |
| `app.go` | the app's state, the view, and the pages' shared parts |
| `pages.go` | the five pages |
| `engines.go` | the downloaders, their presets, and the guess from a link |
| `downloader.go` | the download queue, its progress, and the downloaders' output |
| `info.go` | what an engine's `-J` says about a link |
| `settings.go` | the settings, kept as JSON in the app's data directory |
| `icons.go` | the icons, drawn from SVG paths |

## Before you use it

Download only what the law where you are lets you download, and what the site's
terms allow. The app checks neither.
