package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
)

// downloadPage is the basic downloader: a link, an engine, a preset, and
// the formats the link offers.
func (a *app) downloadPage(c *ui.Context) {
	scrollPage(c, func() {
		card(c, "Link", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.url).Placeholder("https://www.youtube.com/watch?v=…").Grow(1)
				ui.Button(c, "Paste").OnClick(func() {
					a.url = a.clipboard()
					a.guessEngine()
				})
			})
			if guess := engineFor(a.url); a.url != "" && guess != a.engine {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconSearch).FontSize(14).Shrink(0)
					muted(c, "This link looks like "+engineByID(guess).name+".")
					ui.Button(c, "Use it").OnClick(func() { a.setEngine(guess) })
				})
			}
		})

		card(c, "Download options", func() {
			// Local copies: each select writes the choice through its
			// pointer as it returns, and the app takes it from there.
			name := engineByID(a.engine).name
			preset := a.preset
			ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, "Engine").TextColor(c.Theme().TextMuted)
					ui.Select(c, &name, engineNames()).Width(200)
				})
				ui.Column(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, "Format").TextColor(c.Theme().TextMuted)
					ui.Select(c, &preset, presetNames(a.engine)).Grow(1)
				})
			})
			if id := idForName(name); id != "" && id != a.engine {
				a.setEngine(id)
			} else if preset != a.preset && preset != "" {
				a.setPreset(preset)
			}
			ui.TextInput(c, &a.extra).Placeholder("Extra options, e.g. --embed-subs --embed-thumbnail").Grow(1)
			muted(c, "Extra options are split as a shell splits them, and added to the command line.")
			// Subtitles: local copies of the settings, written back once
			// the inputs have applied the change, as a preset is.
			if e := engineByID(a.engine); e.subsArgs != nil {
				embed, langs := a.set.EmbedSubs, a.set.SubLangs
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Checkbox(c, &embed, "Embed the subtitles in the video").Shrink(0)
					ui.TextInput(c, &langs).Placeholder("Languages, e.g. en,ja — empty for the engine's choice").Grow(1).OnChange(func() {
						a.set.SubLangs = langs
						a.save()
					})
				})
				if embed != a.set.EmbedSubs {
					a.set.EmbedSubs = embed
					a.save()
				}
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Button(c, "Get formats").Disabled(a.url == "" || a.fetching).OnClick(a.fetchFormats)
				if a.fetching {
					ui.Spinner(c).Size(18, 18)
				}
				ui.Spacer(c)
				ui.PrimaryButton(c, "Download").Disabled(a.url == "" || a.fetching).OnClick(a.download)
			})
		})

		a.fetchError(c)

		if a.info != nil && a.infoFor == a.url && !a.infoFlat {
			a.formatsCard(c)
		}
	})
}

// subtitlesPage downloads the subtitles of a video, or of every video in a
// playlist, and nothing else. They are rewritten into blocks of two lines
// as they are written: see cleanSRT.
func (a *app) subtitlesPage(c *ui.Context) {
	scrollPage(c, func() {
		card(c, "Link", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.subURL).Placeholder("https://www.youtube.com/watch?v=…").Grow(1)
				ui.Button(c, "Paste").OnClick(func() { a.subURL = a.clipboard() })
			})
			muted(c, "A video, or a playlist and every video in it. yt-dlp fetches them.")
		})

		card(c, "Which languages", func() {
			// A local copy of the setting, written back once the input has
			// applied the change, as the download page's is.
			langs := a.set.SubLangs
			ui.TextInput(c, &langs).Placeholder("Languages, e.g. en,ja — empty for the engine's own choice").Grow(1).OnChange(func() {
				a.set.SubLangs = langs
				a.save()
			})
			muted(c, "Automatic captions are downloaded when a video has no subtitles of its own, and are rewritten into clean blocks of two lines.")
			muted(c, "One .srt per video, in the downloads folder.")
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spacer(c)
				ui.PrimaryButton(c, "Download subtitles").Disabled(a.subURL == "").OnClick(a.downloadSubtitles)
			})
		})
	})
}

// downloadSubtitles queues a link's subtitles, from yt-dlp whatever engine
// the other pages are set to.
func (a *app) downloadSubtitles() {
	url := strings.TrimSpace(a.subURL)
	if url == "" {
		return
	}
	a.mgr.add(&task{
		url:      url,
		engineID: "yt-dlp",
		preset:   "Subtitles only",
		job:      a.command(url, "yt-dlp", "Subtitles only", slices.Clone(subtitleArgs)),
	})
	a.subURL = ""
	a.goTo("library")
}

// fetchError shows why asking an engine about a link failed.
func (a *app) fetchError(c *ui.Context) {
	if a.fetchErr == "" {
		return
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconWarn).FontSize(16).TextColor(c.Theme().Danger).Shrink(0)
		ui.Text(c, a.fetchErr).TextColor(c.Theme().Danger).Grow(1).Selectable()
	})
}

// formatsCard lists the streams a link offers, and downloads those chosen.
func (a *app) formatsCard(c *ui.Context) {
	t := c.Theme()
	info := a.info
	title := fmt.Sprintf("Available formats — %s", info.title)
	if d := duration(info.duration); d != "" {
		title += " (" + d + ")"
	}
	cols := []ui.TableColumn{
		{Title: "Format", Fixed: true, Width: 104},
		{Title: "Ext", Fixed: true, Width: 60},
		{Title: "Resolution", Fixed: true, Width: 116},
		{Title: "Size", Fixed: true, Width: 100, Align: ui.End},
		{Title: "Note"},
	}
	card(c, title, func() {
		if len(info.formats) == 0 {
			muted(c, "This link offers no separate formats. Download it with the options above.")
			return
		}
		a.formatRow.Key = func(i int) any { return info.formats[i].id }
		a.formatRow.Label = func(i int) string { return info.formats[i].id }
		ui.Table(c, &a.formatRow, cols, len(info.formats), func(row, col int) {
			f := info.formats[row]
			switch col {
			case 0:
				chosen := a.chosen[f.id]
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Checkbox(c, &chosen, f.id)
					ui.Text(c, f.id)
				})
				if chosen != a.chosen[f.id] {
					a.chosen[f.id] = chosen
				}
			case 1:
				ui.Text(c, f.ext)
			case 2:
				ui.Text(c, resolution(f))
			case 3:
				ui.Text(c, sizeOf(f))
			case 4:
				ui.Text(c, formatNote(f)).TextColor(t.TextMuted)
			}
		}).MaxHeight(340)
		n := len(a.chosenFormats())
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			label := "Download"
			if n > 0 {
				label = fmt.Sprintf("Download %d stream(s)", n)
			}
			ui.PrimaryButton(c, label).Disabled(n == 0).OnClick(a.downloadFormats)
			if n == 0 {
				muted(c, "Choose the streams to merge, video and audio.")
			}
		})
	})
}

// resolution writes a format's size in pixels.
func resolution(f mediaFormat) string {
	if f.width == 0 || f.height == 0 {
		return ""
	}
	return fmt.Sprintf("%d×%d", f.width, f.height)
}

// sizeOf writes a format's size, or that it is unknown.
func sizeOf(f mediaFormat) string {
	if f.size <= 0 {
		return "unknown"
	}
	return humanSize(f.size)
}

// formatNote says what a format holds.
func formatNote(f mediaFormat) string {
	var parts []string
	if f.note != "" {
		parts = append(parts, f.note)
	}
	switch {
	case f.video && f.audio:
		parts = append(parts, "video + audio")
	case f.video:
		parts = append(parts, "video only")
	case f.audio:
		parts = append(parts, "audio only")
	}
	return strings.Join(parts, ", ")
}

// chosenFormats returns the ids of the formats chosen, in the order the
// link lists them.
func (a *app) chosenFormats() []string {
	if a.info == nil {
		return nil
	}
	var ids []string
	for _, f := range a.info.formats {
		if a.chosen[f.id] {
			ids = append(ids, f.id)
		}
	}
	return ids
}

// chosenEntries returns the ids of the playlist entries chosen.
func (a *app) chosenEntries() []string {
	if a.info == nil {
		return nil
	}
	var ids []string
	for _, e := range a.info.entries {
		if a.chosen[e.id] {
			ids = append(ids, e.id)
		}
	}
	return ids
}

// guessEngine picks the engine the link looks like.
func (a *app) guessEngine() {
	if a.url != "" {
		if guess := engineFor(a.url); guess != a.engine {
			a.setEngine(guess)
		}
	}
}

// download queues the link of the New download page.
func (a *app) download() {
	a.guessEngine()
	url := strings.TrimSpace(a.url)
	if url == "" {
		return
	}
	a.queue(url, a.preset, presetArgs(a.engine, a.preset))
	a.url, a.info, a.chosen = "", nil, map[string]bool{}
	a.goTo("library")
}

// downloadFormats queues the link with the streams chosen in the table.
func (a *app) downloadFormats() {
	ids := a.chosenFormats()
	if len(ids) == 0 || a.info == nil {
		return
	}
	url := strings.TrimSpace(a.url)
	if url == "" {
		url = a.infoFor
	}
	a.queue(url, "Selected streams", []string{"-f", strings.Join(ids, "+")})
	a.chosen = map[string]bool{}
	a.goTo("library")
}

// fetchFormats asks the engine what the link holds, off the UI thread.
func (a *app) fetchFormats() {
	url := strings.TrimSpace(a.url)
	if url == "" || a.fetching {
		return
	}
	a.ask(url, false, a.fetchErr)
}

// batchPage queues many links at once.
func (a *app) batchPage(c *ui.Context) {
	scrollPage(c, func() {
		card(c, "Links", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.batchURL).Placeholder("https://…").Grow(1)
				ui.Button(c, "Add").Disabled(a.batchURL == "").OnClick(func() {
					if u := strings.TrimSpace(a.batchURL); u != "" {
						a.batch = append(a.batch, u)
						a.batchURL = ""
					}
				})
				ui.Button(c, "Paste").OnClick(func() { a.batch = append(a.batch, splitLines(a.clipboard())...) })
				ui.Button(c, "From file…").OnClick(a.importLinks)
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Button(c, "Clear").Disabled(len(a.batch) == 0).OnClick(func() { a.batch = nil })
				ui.Spacer(c)
				if n := len(a.batch); n > 0 {
					ui.PrimaryButton(c, fmt.Sprintf("Download %d link(s)", n)).OnClick(a.downloadBatch)
				}
			})
		})

		if len(a.batch) == 0 {
			emptyState(c, iconBatch, "No links yet. Add them one by one, or import a file with one link per line.")
			return
		}
		a.batchRow.Label = func(i int) string { return a.batch[i] }
		ui.List(c, &a.batchRow, len(a.batch), func(i int) {
			ui.Row(c).Gap(8).Padding(8, 12).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconFile).FontSize(15).TextColor(c.Theme().TextMuted).Shrink(0)
				ui.Text(c, a.batch[i]).SingleLine().Grow(1)
				iconButton(c, iconClose, "Remove", func() { a.batch = append(a.batch[:i], a.batch[i+1:]...) })
			})
		}).MaxHeight(420)
	})
}

// importLinks reads links from a text file the user picks.
func (a *app) importLinks() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  a.win,
			Title:   "Choose a file with links",
			Filters: []mygo.FileFilter{{Name: "Text files", Extensions: []string{"txt", "lst", "csv"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			a.win.Update(func() { a.fetchErr = err.Error() })
			return
		}
		links := splitLines(string(data))
		a.win.Update(func() { a.batch = append(a.batch, links...) })
	}()
}

// downloadBatch queues every link in the batch list.
func (a *app) downloadBatch() {
	for _, u := range a.batch {
		if u = strings.TrimSpace(u); u != "" {
			a.queue(u, a.preset, presetArgs(a.engine, a.preset))
		}
	}
	a.batch = nil
	a.goTo("library")
}

// playlistPage lists what a playlist holds and downloads the entries
// chosen.
func (a *app) playlistPage(c *ui.Context) {
	scrollPage(c, func() {
		card(c, "Playlist link", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.listURL).Placeholder("https://www.youtube.com/playlist?list=…").Grow(1)
				ui.Button(c, "Paste").OnClick(func() { a.listURL = a.clipboard() })
				ui.PrimaryButton(c, "Get list").Disabled(a.listURL == "" || a.fetching).OnClick(a.fetchEntries)
			})
			if a.fetching {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).Size(18, 18)
					muted(c, "Asking "+engineByID(a.engine).name+" what the playlist holds…")
				})
			}
		})

		a.fetchError(c)

		info := a.info
		if info == nil || a.infoFor != a.listURL || !a.infoFlat || len(info.entries) == 0 {
			if !a.fetching && a.fetchErr == "" && info == nil {
				emptyState(c, iconPlaylist, "Paste a playlist link and ask the engine what it holds.")
			}
			return
		}

		cols := []ui.TableColumn{
			{Title: "#", Fixed: true, Width: 56, Align: ui.End},
			{Title: "Title"},
			{Title: "Length", Fixed: true, Width: 88, Align: ui.End},
		}
		card(c, fmt.Sprintf("%d entries — %s", len(info.entries), info.title), func() {
			a.entryRow.Key = func(i int) any { return info.entries[i].id }
			a.entryRow.Label = func(i int) string { return info.entries[i].title }
			ui.Table(c, &a.entryRow, cols, len(info.entries), func(row, col int) {
				e := info.entries[row]
				switch col {
				case 0:
					chosen := a.chosen[e.id]
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Checkbox(c, &chosen, e.title)
						ui.Text(c, fmt.Sprint(row+1)).TextColor(c.Theme().TextMuted)
					})
					if chosen != a.chosen[e.id] {
						a.chosen[e.id] = chosen
					}
				case 1:
					ui.Text(c, e.title).SingleLine()
				case 2:
					ui.Text(c, duration(e.duration)).TextColor(c.Theme().TextMuted)
				}
			}).MaxHeight(420)
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "Select all").OnClick(func() {
					for _, e := range info.entries {
						a.chosen[e.id] = true
					}
				})
				ui.Button(c, "Select none").OnClick(func() {
					for _, e := range info.entries {
						a.chosen[e.id] = false
					}
				})
				ui.Spacer(c)
				n := len(a.chosenEntries())
				label := "Download"
				if n > 0 {
					label = fmt.Sprintf("Download %d entry(ies)", n)
				}
				ui.PrimaryButton(c, label).Disabled(n == 0).OnClick(a.downloadEntries)
			})
		})
	})
}

// fetchEntries asks the engine what a playlist holds, off the UI thread.
func (a *app) fetchEntries() {
	url := strings.TrimSpace(a.listURL)
	if url == "" || a.fetching {
		return
	}
	a.ask(url, true, a.fetchErr)
}

// ask runs an engine's info command off the UI thread and shows what it
// says. flat asks for a playlist's entries instead of one video's
// formats.
func (a *app) ask(url string, flat bool, _ string) {
	e := engineByID(a.engine)
	if !e.formats {
		a.fetchErr = e.name + " cannot list what a link holds. Download it with the options above."
		return
	}
	a.fetching, a.fetchErr, a.info, a.chosen = true, "", nil, map[string]bool{}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				a.log.logger().Error("panic while asking about a link", "url", url,
					"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				a.win.Update(func() {
					a.fetching = false
					a.fetchErr = fmt.Sprintf("the app failed while asking about this link: %v", r)
				})
			}
		}()
		info, err := fetchInfo(e, a.set.Binaries[e.id], url, flat)
		a.win.Update(func() {
			a.fetching = false
			switch {
			case err != nil:
				a.fetchErr = err.Error()
				a.log.logger().Warn("asking about a link failed", "url", url,
					"engine", e.id, "err", err)
			case flat && len(info.entries) == 0:
				a.fetchErr = "That link holds no playlist."
			default:
				a.info, a.infoFor, a.infoFlat = info, url, flat
			}
		})
	}()
}

// downloadEntries queues the playlist entries chosen.
func (a *app) downloadEntries() {
	if a.info == nil {
		return
	}
	for _, id := range a.chosenEntries() {
		for _, e := range a.info.entries {
			if e.id == id {
				a.queue(e.url, a.preset, presetArgs(a.engine, a.preset))
			}
		}
	}
	a.chosen = map[string]bool{}
	a.goTo("library")
}

// libraryPage shows the downloads of this session.
func (a *app) libraryPage(c *ui.Context) {
	tasks := a.filtered()

	// Fills the window so the list scrolls itself rather than growing
	// without end inside the page's own scroll.
	ui.Column(c).Fill().Padding(4, 26, 26).Gap(12).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &a.filter, "All", "Active", "Completed", "Failed").Label("Show")
			ui.Spacer(c)
			ui.Button(c, "Clear finished").Disabled(!a.anyFinished()).OnClick(a.mgr.clearFinished)
		})

		if len(tasks) == 0 {
			ui.Scroll(c).Grow(1).Children(func() {
				emptyState(c, iconDownload, "Nothing here yet.")
				ui.Row(c).Justify(ui.Center).Children(func() {
					ui.Button(c, "Add a download").OnClick(func() { a.goTo("download") })
				})
			})
			return
		}

		a.lib.Label = func(i int) string { return tasks[i].title }
		ui.List(c, &a.lib, len(tasks), func(i int) {
			a.taskRow(c, tasks[i])
		}).Grow(1)

		ui.Modal(c, &a.detailsOpen, func() {
			if tsk, ok := a.mgr.get(a.details); ok {
				a.taskDetails(c, tsk)
			}
		})
	})
}

// filtered returns the tasks the library's filter shows.
func (a *app) filtered() []task {
	all := a.mgr.snapshot()
	out := make([]task, 0, len(all))
	for _, tsk := range all {
		switch a.filter {
		case 1: // Active
			if tsk.state.stopped() {
				continue
			}
		case 2: // Completed
			if tsk.state != statusDone {
				continue
			}
		case 3: // Failed
			if tsk.state != statusFailed {
				continue
			}
		}
		out = append(out, tsk)
	}
	return out
}

// anyFinished reports whether any task has stopped.
func (a *app) anyFinished() bool {
	for _, tsk := range a.mgr.snapshot() {
		if tsk.state.stopped() {
			return true
		}
	}
	return false
}

// taskRow is one download in the library's list.
func (a *app) taskRow(c *ui.Context, tsk task) {
	t := c.Theme()
	row := ui.Row(c).Gap(12).Padding(10, 14).Radius(8).Border(1, t.Border).Background(t.Surface)
	row.Children(func() {
		ui.Column(c).Gap(6).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tsk.title).Bold().SingleLine().Grow(1)
				ui.Text(c, tsk.state.String()).TextColor(statusColor(c, tsk.state)).Shrink(0)
			})
			if !tsk.state.stopped() {
				ui.Progress(c, tsk.percent/100).Height(6)
			}
			var facts []string
			if tsk.size != "" {
				facts = append(facts, tsk.size)
			}
			if tsk.speed != "" {
				facts = append(facts, tsk.speed)
			}
			if tsk.eta != "" {
				facts = append(facts, "ETA "+tsk.eta)
			}
			facts = append(facts, engineByID(tsk.engineID).name)
			if tsk.err != "" {
				facts = append(facts, tsk.err)
			}
			ui.Text(c, strings.Join(facts, " · ")).TextColor(t.TextMuted).SingleLine()
		})
		ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
			switch tsk.state {
			case statusRunning, statusQueued:
				iconButton(c, iconPause, "Pause", func() { a.mgr.pause(tsk.id) })
			case statusPaused, statusFailed:
				iconButton(c, iconResume, "Resume", func() { a.mgr.resume(tsk.id) })
			case statusDone:
				if p := tsk.path(); p != "" && exists(p) {
					iconButton(c, iconOpen, "Open the file", func() { _ = mygo.Shell.OpenPath(p) })
					iconButton(c, iconReveal, "Show in folder", func() { mygo.Shell.ShowItemInFolder(p) })
				}
			}
			iconButton(c, iconRefresh, "Start over", func() { a.mgr.retry(tsk.id) })
			iconButton(c, iconFile, "Details", func() { a.details, a.detailsOpen = tsk.id, true })
			iconButton(c, iconTrash, "Remove", func() { a.mgr.remove(tsk.id) })
		})
	})
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy link").Chosen() {
			a.setClipboard(tsk.url)
		}
		p := tsk.path()
		if p != "" && exists(p) {
			m.Separator()
			if m.Item("Open the file").Chosen() {
				_ = mygo.Shell.OpenPath(p)
			}
			if m.Item("Show in folder").Chosen() {
				mygo.Shell.ShowItemInFolder(p)
			}
		}
		m.Separator()
		if m.Item("Details…").Chosen() {
			a.details, a.detailsOpen = tsk.id, true
		}
		if m.Item("Remove").Chosen() {
			a.mgr.remove(tsk.id)
		}
	})
}

// statusColor says how a task's state reads.
func statusColor(c *ui.Context, s status) ui.Color {
	t := c.Theme()
	switch s {
	case statusDone:
		return t.Success
	case statusFailed:
		return t.Danger
	case statusRunning, statusQueued:
		return t.Accent
	}
	return t.TextMuted
}

// taskDetails shows what a downloader last said.
func (a *app) taskDetails(c *ui.Context, tsk task) {
	t := c.Theme()
	ui.Column(c).Width(600).MaxHeight(440).Gap(12).Padding(20).Radius(12).
		Background(t.Background).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, tsk.title).FontSize(16).Bold().Grow(1)
			ui.Text(c, tsk.state.String()).TextColor(statusColor(c, tsk.state)).Shrink(0)
		})
		var facts []string
		facts = append(facts, engineByID(tsk.engineID).name)
		if tsk.size != "" {
			facts = append(facts, tsk.size)
		}
		if !tsk.endedAt.IsZero() {
			facts = append(facts, tsk.endedAt.Format("15:04:05"))
		}
		ui.Text(c, strings.Join(facts, " · ")).TextColor(t.TextMuted)
		ui.Text(c, tsk.url).TextColor(t.TextMuted).Selectable()
		if tsk.err != "" {
			ui.Text(c, tsk.err).TextColor(t.Danger).Selectable()
		}
		ui.Column(c).Grow(1).MinHeight(120).Padding(10).Radius(8).Background(t.Surface).Children(func() {
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(2).Children(func() {
					if len(tsk.log) == 0 {
						muted(c, "No output yet.")
					}
					for _, line := range tsk.log {
						ui.Text(c, line).Font("monospace").Selectable()
					}
				})
			})
		})
		ui.Row(c).Justify(ui.End).Gap(8).Children(func() {
			if p := tsk.path(); p != "" && exists(p) {
				ui.Button(c, "Show in folder").OnClick(func() { mygo.Shell.ShowItemInFolder(p) })
			}
			ui.PrimaryButton(c, "Close").OnClick(func() { a.detailsOpen = false })
		})
	})
}

// settingsPage is where the downloaders and the destination are chosen.
func (a *app) settingsPage(c *ui.Context) {
	t := c.Theme()
	scrollPage(c, func() {
		card(c, "Where downloads go", func() {
			ui.Form(c, func() {
				ui.Field(c, "Folder", func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c, &a.set.DownloadDir).Grow(1)
						ui.Button(c, "Choose…").OnClick(a.chooseDir)
					})
				})
				ui.Field(c, "File name", func() {
					ui.TextInput(c, &a.set.Template).Grow(1)
				}).Description("A template the engine understands, e.g. %(title)s [%(id)s].%(ext)s")
				ui.Field(c, "At once", func() {
					// A local copy: the number input writes through it as it
					// returns, and the app takes the whole number it holds.
					n := float64(a.set.MaxActive)
					ui.NumberInput(c, &n, 1, 16, 1).Width(140)
					if v := int(n); v != a.set.MaxActive {
						a.set.MaxActive = v
						a.mgr.setMax(v)
					}
				})
				ui.Field(c, "Cookies file", func() {
					ui.Row(c).Gap(8).Children(func() {
						ui.TextInput(c, &a.set.Cookies).Grow(1)
						ui.Button(c, "Choose…").OnClick(a.chooseCookies)
					})
				}).Description("For sites that ask you to sign in. Passed to the engines that take one.")
				ui.Field(c, "Proxy", func() {
					ui.TextInput(c, &a.set.Proxy).Placeholder("http://127.0.0.1:8080").Grow(1)
				})
				ui.Field(c, "Keep awake", func() {
					ui.Switch(c, &a.set.KeepAwake)
				}).Description("While downloads run.")
				ui.Field(c, "Notify", func() {
					ui.Switch(c, &a.set.Notify)
				}).Description("When a download finishes or fails.")
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.PrimaryButton(c, "Save").OnClick(func() {
					a.save()
					c.Toast("Settings saved")
				})
				ui.Button(c, "Restore defaults").OnClick(a.restoreDefaults)
				ui.Spacer(c)
				muted(c, "Kept in the app's data directory.")
			})
		})

		card(c, "Downloaders", func() {
			cols := []ui.TableColumn{
				{Title: "Name", Fixed: true, Width: 140},
				{Title: "State", Fixed: true, Width: 110},
				{Title: "Executable"},
			}
			a.engineRow.Key = func(i int) any { return engines[i].id }
			ui.Table(c, &a.engineRow, cols, len(engines), func(row, col int) {
				e := engines[row]
				// A local copy of the path: OnChange commits the edit once
				// the input has applied it.
				path := a.set.Binaries[e.id]
				switch col {
				case 0:
					ui.Text(c, e.name).SingleLine()
				case 1:
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						if e.installed(path) {
							ui.Icon(c, iconCheck).FontSize(14).TextColor(t.Success).Shrink(0)
							ui.Text(c, "ready")
						} else {
							ui.Icon(c, iconWarn).FontSize(14).TextColor(t.Warning).Shrink(0)
							ui.Text(c, "not found")
						}
					})
				case 2:
					ui.TextInput(c, &path).Placeholder(e.bin).OnChange(func() {
						if path == "" {
							delete(a.set.Binaries, e.id)
							return
						}
						a.set.Binaries[e.id] = path
					})
				}
			})
			muted(c, "Leave the path empty to find the downloader in PATH. Each engine is a program you install yourself.")
		})
	})
}

// restoreDefaults puts the settings back as they were first.
func (a *app) restoreDefaults() {
	d := defaultSettings()
	a.set.DownloadDir, a.set.Template = d.DownloadDir, d.Template
	a.set.MaxActive, a.set.Cookies, a.set.Proxy = d.MaxActive, "", ""
	a.set.KeepAwake, a.set.Notify = d.KeepAwake, d.Notify
	a.save()
}

// chooseDir asks where downloads go.
func (a *app) chooseDir() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    a.win,
			Title:     "Choose a download folder",
			Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		dir := paths[0]
		a.win.Update(func() { a.set.DownloadDir = dir })
	}()
}

// chooseCookies asks for a cookies file.
func (a *app) chooseCookies() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:  a.win,
			Title:   "Choose a cookies file",
			Filters: []mygo.FileFilter{{Name: "Cookies", Extensions: []string{"txt"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		file := paths[0]
		a.win.Update(func() { a.set.Cookies = file })
	}()
}

// logsPage is what the app did, and what went wrong: the tail of the log,
// with the file it is written to for anything the memory has already
// forgotten.
func (a *app) logsPage(c *ui.Context) {
	t := c.Theme()
	entries := a.log.tail()

	ui.Column(c).Fill().Padding(4, 26, 26).Gap(12).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Segmented(c, &a.logFilter, "All", "Errors", "Warnings").Label("Show")
			ui.Spacer(c)
			ui.Button(c, "Copy").Disabled(len(entries) == 0).OnClick(func() {
				a.setClipboard(a.log.text())
				c.Toast("Log copied")
			})
			ui.Button(c, "Open log file").OnClick(a.openLogFile)
		})

		if len(entries) == 0 {
			emptyState(c, iconLogs, "Nothing logged yet.")
			return
		}

		shown := a.filteredLog(entries)
		if len(shown) == 0 {
			ui.Column(c).Padding(24).AlignItems(ui.Center).Children(func() {
				muted(c, "Nothing of that kind in the log.")
			})
			return
		}
		// A log follows its end, as a chat does, until the user scrolls
		// away to read something.
		a.logRow.FollowEnd = true
		a.logRow.Key = func(i int) any { return shown[i].at.UnixNano() }
		ui.List(c, &a.logRow, len(shown), func(i int) {
			a.logEntry(c, shown[i])
		}).Grow(1)

		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconFile).FontSize(13).TextColor(t.TextMuted).Shrink(0)
			ui.Text(c, a.log.path).TextColor(t.TextMuted).SingleLine().Grow(1).Selectable()
		})
	})
}

// logRow2 is one entry of the log.
func (a *app) logEntry(c *ui.Context, e entry) {
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(4, 8).AlignItems(ui.Start).Children(func() {
		ui.Text(c, e.at.Format("15:04:05")).Font("monospace").FontSize(12).
			TextColor(t.TextMuted).Shrink(0)
		ui.Text(c, e.level.String()).FontSize(12).TextColor(levelColor(c, e.level)).
			Width(56).Shrink(0)
		ui.Column(c).Gap(2).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, e.msg).SingleLine()
			for _, attr := range e.attrs {
				ui.Text(c, attr).Font("monospace").FontSize(12).
					TextColor(t.TextMuted).SingleLine().Selectable()
			}
		})
	}).ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy this entry").Chosen() {
			a.setClipboard(e.String())
		}
	})
}

// levelColor says how a log entry's level reads.
func levelColor(c *ui.Context, l slog.Level) ui.Color {
	t := c.Theme()
	switch {
	case l >= slog.LevelError:
		return t.Danger
	case l >= slog.LevelWarn:
		return t.Warning
	}
	return t.TextMuted
}

// filteredLog returns the entries the Logs page's filter shows.
func (a *app) filteredLog(entries []entry) []entry {
	var out []entry
	for _, e := range entries {
		switch a.logFilter {
		case 1: // Errors
			if e.level < slog.LevelError {
				continue
			}
		case 2: // Warnings
			if e.level < slog.LevelWarn || e.level >= slog.LevelError {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// openLogFile shows the log file in the system's file manager.
func (a *app) openLogFile() {
	if a.log == nil || a.log.path == "" {
		return
	}
	mygo.Shell.ShowItemInFolder(a.log.path)
}

// aboutPage says what the app is and what it is not.
func (a *app) aboutPage(c *ui.Context) {
	t := c.Theme()
	scrollPage(c, func() {
		card(c, "VDLP", func() {
			ui.Text(c, "A front end for the command line downloaders, as Media Downloader is, drawn by MyGo in Go.")
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				if v := mygo.App.Version(); v != "" {
					ui.Text(c, "Version "+v).TextColor(t.TextMuted)
				}
				ui.Button(c, "Check for updates").OnClick(updater.CheckForUpdates)
			})
			ui.Text(c, "It drives yt-dlp, youtube-dl, gallery-dl, lux, you-get, svtplay-dl, aria2c, wget and Safari Books. Each is a program you install yourself; the app only tells it what to fetch.").TextColor(t.TextMuted)
			ui.Row(c).Gap(8).Children(func() {
				ui.Button(c, "yt-dlp").OnClick(func() { _ = mygo.Shell.OpenExternal("https://github.com/yt-dlp/yt-dlp") })
				ui.Button(c, "gallery-dl").OnClick(func() { _ = mygo.Shell.OpenExternal("https://github.com/mikf/gallery-dl") })
				ui.Button(c, "MyGo").OnClick(func() { _ = mygo.Shell.OpenExternal("https://mygo.egoist.dev") })
				ui.Button(c, "Media Downloader").OnClick(func() { _ = mygo.Shell.OpenExternal("https://github.com/mhogomchungu/media-downloader") })
			})
		})
		card(c, "Before you use it", func() {
			ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconWarn).FontSize(16).TextColor(t.Warning).Shrink(0)
				ui.Text(c, "Download only what the law where you are lets you download, and what the site's terms allow. The app checks neither.").TextColor(t.TextMuted).Grow(1)
			})
		})
		card(c, "This session", func() {
			tasks := a.mgr.snapshot()
			ui.Row(c).Gap(24).Children(func() {
				stat(c, "Queued", len(a.byStatus(statusQueued, tasks)))
				stat(c, "Running", len(a.byStatus(statusRunning, tasks)))
				stat(c, "Completed", len(a.byStatus(statusDone, tasks)))
				stat(c, "Failed", len(a.byStatus(statusFailed, tasks)))
			})
		})
	})
}

// byStatus counts the tasks in one state.
func (a *app) byStatus(s status, tasks []task) []task {
	var out []task
	for _, tsk := range tasks {
		if tsk.state == s {
			out = append(out, tsk)
		}
	}
	return out
}

// stat is a number with its label.
func stat(c *ui.Context, label string, n int) {
	ui.Column(c).Gap(2).Children(func() {
		ui.Textf(c, "%d", n).FontSize(22).Bold()
		ui.Text(c, label).TextColor(c.Theme().TextMuted)
	})
}

// fetchInfo asks an engine what a link holds, with -J, and reads the JSON
// it prints. flat lists a playlist's entries instead of its formats.
func fetchInfo(e engine, override, url string, flat bool) (*mediaInfo, error) {
	args := []string{"-J", "--no-warnings"}
	if flat {
		args = append(args, "--flat-playlist")
	} else {
		args = append(args, "--no-playlist")
	}
	args = append(args, url)
	out, err := exec.Command(e.binary(override), args...).Output()
	if err != nil {
		return nil, infoError(e, err)
	}
	return parseInfo(out, flat, url)
}

// infoError says why an engine's info command failed, with the last thing
// it printed when that says more than the exit status.
func infoError(e engine, err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		if lines := splitLines(string(exit.Stderr)); len(lines) > 0 {
			return fmt.Errorf("%s: %s", e.name, lines[len(lines)-1])
		}
	}
	return fmt.Errorf("%s: %w", e.name, err)
}
