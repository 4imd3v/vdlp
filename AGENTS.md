# VDLP

VDLP is a front end for the command line media downloaders — yt-dlp,
youtube-dl, gallery-dl, lux, you-get, svtplay-dl, aria2c, wget and Safari
Books — as [Media Downloader](https://github.com/mhogomchungu/media-downloader)
is, with a native interface [MyGo](https://mygo.egoist.dev) draws itself in
Go. There is no web page, no frontend build and no cgo: one Go module, and
`mygo build` packages a binary for Windows, Linux and macOS.

**Read [`.agents/skills/mygo-maintenance/SKILL.md`](.agents/skills/mygo-maintenance/SKILL.md)
first.** It holds the MyGo rules this app follows — UI lifetime and identity,
handles, input and actions, state and rendering, and how to verify. This file
holds what is specific to *this* repo. When the two disagree, the skill wins on
MyGo usage and this file wins on where things live.

MyGo is pinned in `go.mod` (`v0.7.4` as this was written). Read the pinned
source, not the website: `go list -m -f '{{.Dir}}' github.com/egoist/mygo`,
then `ui/doc.go` and the implementation comments for whatever you touch.
`docs/ui/*.md` in that module documents every widget.

The skill at `.agents/skills/mygo-maintenance/SKILL.md` is MyGo's own, put
there by `go tool mygo install-skills .`. Update it with that command rather
than by hand, so it stays in step with the pinned version.

## Memory

If the NWron MCP is available, search project `vdlp` before starting.

Only memorize committed work:

1. Run `git status --short`. If anything is uncommitted, do not write a memory.
   Ask the user to commit first, or wait for the commit.
2. Record the commit hash and describe the state at that commit. Never claim work
   that only exists in the working tree.
3. When a change alters a decision already in memory, update or supersede that
   memory after the new commit instead of adding a second copy. Leave stale
   memories alone until the replacement is committed.
4. Never commit, tag or publish just to make a memory writable.

If the user asks to memorize while the tree is dirty, say so and stop at the
commit step unless they explicitly asked you to commit.

## Hard rules

- **No cgo.** Everything builds with `CGO_ENABLED=0`.
- **No web frontend, no Bun.** The window shows `ui.View`, never a URL. Do not
  add a `package.json`, a Vite config, or a `mygo.config.ts`.
- **The view is a pure function of state.** It runs several times per frame.
  Start no download, write no file and call no `mygo.Shell`/`mygo.Dialog` from
  it directly: bind an action (`OnClick`, `OnChange`) or poll a query
  (`Clicked`, `Submitted`) and act there.
- **Slow work leaves the UI thread** and comes back through `Window.Update`,
  which is asynchronous. The view reads state on the main thread only.
- **Keep `Context` out of app state and out of goroutines.** Capture
  `c.Services()` once for the clipboard (see `app.haveServices`).
- **Widget state lives in the app struct, never in the view.** A
  `ui.ListState` built fresh each frame loses its scroll position and its
  selection. Every list in this app has a field for it.

## Layout

| File | What it holds |
|---|---|
| `main.go` | the window, the app menus, and quitting |
| `app.go` | the app's state, the view, the sidebar, and the parts the pages share |
| `pages.go` | the seven pages |
| `engines.go` | the downloaders, their presets, and the guess from a link's host |
| `downloader.go` | the download queue, the task, and folding a downloader's output |
| `info.go` | what an engine's `-J` says about a link |
| `settings.go` | the settings, kept as JSON in the app's data directory |
| `logs.go` | the log: a file on disk, and the tail the Logs page shows |
| `icons.go` | the icons, drawn from SVG paths |

`app` is the state the window shows. `pages` (in `app.go`) is the sidebar's
destinations, in order: Cmd+1…Cmd+7 go to them, and the `Go` menu mirrors it.
Adding a page takes **two** edits, and forgetting the second leaves a page that
renders its heading and nothing else — which is what happened to `logs` once:
an entry in `pages` and an icon in `pageIcon`, *and* a `case` in `app.view`.
`TestRenderPages` walks `pages` and fails on a page showing fewer than
`minPageTexts` texts, which is what a missing `case` looks like. It only
asserts this because it used to write the PNG and check nothing, so a blank
page passed silently; if you change the test, keep an assertion in it.

## Invariants that are easy to break

These are the rules this codebase depends on. Each one is load-bearing; breaking
it produces a bug that is hard to see from the diff.

**The view never touches a live task.** A download's fields are written by its
goroutine under the manager's mutex. `manager.snapshot()` and `manager.get()`
return *copies*, so the view can read them on the UI thread. Do not hand a
`*task` to the view, and do not read `a.mgr.tasks` directly — if you need a new
field shown, add it to `task` (it is copied wholesale) and read it from the copy.

**A download's command is made when it is queued, not when it runs.**
`app.command` builds a `job` (binary, arguments, folder, proxy, resume flag)
from the settings, and `task.job` carries it. The view may change the settings
while a download runs; the running download must not read them. `manager.run`
reads `t.job` and `t.resume` once, under the mutex, and then touches no shared
state until it reports back.

**The way to stop a download is in place before the task is said to be
running.** `manager.startNext` creates the context, stores its cancel on the
task, marks the task running, and hands *both* the context and the cancel to
`go m.run(...)` as arguments. It must not let `run` read them off the task
afterwards: a pause arriving between the two would be lost and the download
would run forever. This race is not theoretical — it was a real bug.

**A paused download stays paused.** `finish` must not re-queue a task the user
stopped, and `startNext` only starts `statusQueued`. `pause` sets
`statusPaused`; only `resume` moves it back. Resuming continues the partial
file, because every engine that can resume gets `resumeFlag` (`-c`) — that is
how a stopped download picks up where it left off on every platform.

**A download's output is read to its end, and `Wait` must not close the pipe
under the reader.** `cmd.StdoutPipe` hands its read end to `Wait`, which closes
it the moment the command exits — cutting the last lines off and losing the
file the downloader said it wrote (the merge line, which names the real file,
comes after the one naming the stream). `run` therefore makes its own
`os.Pipe`, gives the write end to the command, closes its own copy, reads to
the end, and only then reaps.

**A killed downloader cannot stall the queue.** The same read is bounded by
`waitCutoff`: a downloader's child (ffmpeg merging formats, say) can outlive it
and hold the write end open, so past the bound the read end is closed under the
reader and the task keeps what it has. `cmd.WaitDelay` covers the reap.

**`ui.Progress` takes 0…1; `task.percent` is 0…100**, or `-1` while the length
is unknown (which the progress bar reads as "of unknown length"). The view
divides by 100.

**Bound values take a local copy when the app must react.** A select or number
input writes through its pointer as its constructor returns, so
`ui.Select(c, &name, ...)` followed by `if id := idForName(name); ...` sees the
user's choice. Writing a derived value straight back into app state inside the
build loses it before its notice. See `settingsPage` and `downloadPage`.

**`ui.ListState` is per list and lives in `app`.** Set `Key` and `Label` on it
each frame before building the list. A list without a size, inside a page that
scrolls, builds every row: give it a `MaxHeight`, or put the page outside the
scroll and `Fill` the column (which is what `libraryPage` does).

## Conventions

- Pages that read as a form wrap their body in `scrollPage(c, func(){…})`. The
  library is the exception: it fills the window so its list scrolls itself.
- `card(c, title, body)` groups controls; `muted(c, s)` is secondary text;
  `emptyState(c, icon, text)` is what a page shows with nothing to list.
- `iconButton(c, icon, label, fn)` is a button showing only an icon. Always
  give it a `Label`: that is what assistive technology reads and what
  `ui.NewTester` finds it by.
- Errors from the engines are shown in `fetchError`, and a task's failure in
  its row. Do not `log.Fatal` on a download that failed: it belongs to the user
  to see and retry.
- **Log through `slog`, never `fmt.Println`.** `app.log` holds the handler; a
  manager logs through `m.log()`, a page through `a.log.logger()`. A record
  reaches both the file (a line of JSON, so a tool can read it) and the tail
  the Logs page shows. Every failure path logs what it took: the URL, the
  binary and its full argv, the folder, the error, and the downloader's last
  line of output. An error is passed as `err`, which the handler renders as
  its message rather than as `{}`.
- **Recover panics in goroutines and log them.** `run` and the fetch in `ask`
  both do, with `debug.Stack()`. A panic that is only printed to the terminal
  is invisible to the user and to anyone reading the log afterwards.
- Icons are SVG paths in `icons.go`, parsed once at package level with
  `ui.MustParseSVG`. Add one there, do not build SVGs in the view.
- Comments say *why*, one line, when the code cannot show it. The invariants
  above are commented where they are enforced.

## Adding a downloader

Add an `engine` to `engines` (`engines.go`) and nothing else — the settings
table, the engine select, the format and playlist pages and the URL guess all
read that table. Fill in:

- `id`, `name`, `bin` — the id is stored in the settings, so do not rename it
  once shipped.
- `presets` — at least one; the first is the default.
- `resumeFlag` — `-c` for most; `""` when the engine cannot continue a partial
  file (pause then restarts from the beginning).
- `formats` — `true` when the engine answers `-J` as yt-dlp does, which is what
  the format and playlist pages need. `false` hides those two pages for it.
- `outFlag`, `cookiesFlag`, `lineFlag`, `extra` — as the engine takes them;
  `""` where it has none. `lineFlag` is what makes progress print one line at a
  time rather than a carriage-return stream.

Then add its hosts to `routes` so a link finds it, and a test case to
`TestEngineFromLink`. `TestEnginesAreUsable` fails on an engine with a
duplicate id, no binary or no presets.

## Verifying

```sh
go test ./...                 # the view and the queue, without a window
CGO_ENABLED=1 go test -race ./...   # the queue is concurrent: run this too
go tool mygo vet .            # stored build values and captured goroutines
go vet ./... && gofmt -l .    # both must be quiet
GOOS=windows go build ./... && GOOS=darwin go build ./...   # cross-compile
go tool mygo dev              # build and run, rebuilding as you edit
go tool mygo build            # package it for this machine
```

`go test -race` is not optional after touching `downloader.go`: the queue's
correctness is in its locking, and the races it has found were real.

Tests drive the view through `ui.NewTester`, which clicks and types as a user
would — no window, no display, no sleeping (advance the tester's clock
instead). `manager_test.go` runs real downloads against a shell script written
to a temp dir, so a change to the queue is checked against a downloader that
actually prints progress and exits. `logs_test.go` checks the handler writes
both ways and that a panic is logged rather than fatal. `zrender_test.go` draws
every page to a PNG with `ui.Render`, to look at the layout without a window,
and fails on a page whose body never built:
`VDLP_RENDER_DIR=/tmp go test -run TestRenderPages .`

A change that is not covered by a test needs a reason. New non-trivial logic —
a branch, a parser, money, a new script — leaves one small test behind.

## What is not here

- **Persistence of the download list.** The library is this session's. On quit
  the queue is stopped and the tasks are gone; only the settings survive.
- **Per-engine install.** The app finds each downloader in `PATH`, or at the
  path the user sets. It does not download or update them.
- **The system proxy.** `settings.Proxy` goes to the downloaders' environment
  as `HTTP_PROXY`/`HTTPS_PROXY`/`ALL_PROXY`; the app does not read the system's
  own proxy settings.
- **Real-hardware verification.** Nothing has been clicked through on Windows
  or macOS. Assume a UI change needs looking at on the platform it is for.
- **The log's own failures.** If the log file cannot be opened the app logs to
  nowhere and carries on, so a machine with an unwritable logs directory loses
  the trace silently. The Logs page would show only what the current run has
  in memory, and say nothing about what it could not write.
