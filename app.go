package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// pages are the sidebar's destinations, in order: Cmd+1 to Cmd+5 go to
// them.
var pages = []struct{ id, title, blurb string }{
	{"download", "New download", "Paste a link, pick a format, download it."},
	{"batch", "Batch download", "Many links at once, from the clipboard or a file."},
	{"playlist", "Playlist", "List what a playlist holds and download what you choose."},
	{"library", "Library", "The downloads of this session, with their progress."},
	{"settings", "Settings", "Where the downloaders are, and where they write."},
	{"logs", "Logs", "What the app did, and what went wrong."},
	{"about", "About", "What this app is, and what it is not."},
}

func pageByID(id string) *struct{ id, title, blurb string } {
	for i := range pages {
		if pages[i].id == id {
			return &pages[i]
		}
	}
	return &pages[0]
}

// app is the state the window shows, and the code the view calls. Its
// fields hold data and widget state; never an element or a Context.
type app struct {
	win *mygo.Window
	mgr *manager
	set settings
	log *appLog

	// page is the sidebar's choice.
	page string

	// The New download page.
	url    string
	engine string
	preset string
	extra  string

	// The Batch page.
	batch    []string // the links waiting to download
	batchURL string
	batchRow ui.ListState

	// The Playlist page.
	listURL string

	// What the app last asked an engine about a link: its formats, or the
	// entries of its playlist, and the ids the user chose among them.
	fetching  bool
	fetchErr  string
	info      *mediaInfo
	infoFor   string
	infoFlat  bool
	chosen    map[string]bool
	formatRow ui.ListState
	entryRow  ui.ListState

	// The Library page.
	filter      int
	lib         ui.ListState
	details     int64 // the task whose log the dialog shows
	detailsOpen bool

	// The Settings page.
	engineRow ui.ListState

	// The Logs page.
	logRow    ui.ListState
	logFilter int

	// services are captured once, for the clipboard, and used from the
	// view's actions rather than from the Context itself.
	services     ui.Services
	haveServices bool
}

// mediaInfo is what an engine's info command says about a link.
type mediaInfo struct {
	title    string
	duration float64
	formats  []mediaFormat
	entries  []mediaEntry
}

// mediaFormat is one stream a link offers.
type mediaFormat struct {
	id, ext, note string
	width, height int
	size          int64
	audio, video  bool
}

// mediaEntry is one video of a playlist.
type mediaEntry struct {
	id, title, url string
	duration       float64
}

func newApp(set settings) *app {
	lg, err := openLog()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vdlp: opening the log:", err)
	}
	log := lg.logger()
	a := &app{
		set:    set,
		page:   pages[0].id,
		engine: set.Engine,
		preset: set.Presets[set.Engine],
		chosen: map[string]bool{},
		log:    lg,
	}
	log.Info("started", "version", mygo.App.Version(), "engine", a.engine,
		"folder", a.set.DownloadDir, "atOnce", a.set.MaxActive, "log", lg.path)
	if a.preset == "" {
		a.preset = engineByID(a.engine).presets[0].name
	}
	a.mgr = newManager(set.MaxActive, a.command)
	a.mgr.logger = log
	a.mgr.publish = func() { a.win.Update(func() {}) }
	a.mgr.awake = func() func() {
		if !a.set.KeepAwake {
			return func() {}
		}
		return mygo.Power.KeepAwake("Downloading media", false)
	}
	a.mgr.notify = func(t task) {
		if !a.set.Notify {
			return
		}
		body := fmt.Sprintf("%s — %s", t.state, engineByID(t.engineID).name)
		if t.state == statusFailed {
			body = t.err
			if body == "" {
				body = "The download failed."
			}
		}
		n := mygo.NewNotification(mygo.NotificationOptions{Title: t.title, Body: body})
		n.OnClick(func() { a.win.Show(); a.win.Focus() })
		_ = n.Show()
	}
	return a
}

// command builds what a download runs with, from the settings as they are
// now. It runs on the UI thread, as the view does, so that the download
// itself never reads settings the user is changing.
func (a *app) command(url, engineID, preset string, presetArgs []string) job {
	e := engineByID(engineID)
	args := append([]string{}, presetArgs...)
	if e.outFlag != "" && a.set.Template != "" {
		args = append(args, e.outFlag, a.set.Template)
	}
	args = append(args, e.extra...)
	if e.lineFlag != "" {
		args = append(args, e.lineFlag)
	}
	if a.set.Cookies != "" && e.cookiesFlag != "" {
		args = append(args, e.cookiesFlag, a.set.Cookies)
	}
	args = append(args, splitArgs(a.extra)...)
	args = append(args, url)
	return job{
		bin:        e.binary(a.set.Binaries[e.id]),
		args:       args,
		dir:        a.set.DownloadDir,
		proxy:      a.set.Proxy,
		resumeFlag: e.resumeFlag,
	}
}

// queue adds a download with the engine and the options the page shows.
func (a *app) queue(url, preset string, presetArgs []string) {
	a.mgr.add(&task{
		url:      url,
		engineID: a.engine,
		preset:   preset,
		job:      a.command(url, a.engine, preset, presetArgs),
	})
}

// presetArgs returns the arguments of a preset of an engine, or nil.
func presetArgs(engineID, name string) []string {
	for _, p := range engineByID(engineID).presets {
		if p.name == name {
			return p.args
		}
	}
	return nil
}

// presetNames returns the names of an engine's presets.
func presetNames(engineID string) []string {
	ps := engineByID(engineID).presets
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.name
	}
	return names
}

// setEngine changes the engine the pages use, taking the preset last used
// with it.
func (a *app) setEngine(id string) {
	if id == a.engine {
		return
	}
	a.engine = id
	a.preset = a.set.Presets[id]
	if a.preset == "" {
		a.preset = engineByID(id).presets[0].name
	}
	a.set.Presets[id] = a.preset
	a.info, a.fetchErr = nil, ""
	a.save()
}

// setPreset remembers a preset for the engine using it.
func (a *app) setPreset(name string) {
	a.preset = name
	a.set.Presets[a.engine] = name
	a.save()
}

// save writes the settings, reporting a failure in the log only: the app
// keeps working without them.
func (a *app) save() {
	if err := a.set.save(); err != nil {
		a.log.logger().Error("saving the settings", "err", err)
	}
	a.mgr.setMax(a.set.MaxActive)
}

// goTo shows a page.
func (a *app) goTo(id string) { a.page = id }

// clipboard returns the clipboard's text, for the Paste buttons.
func (a *app) clipboard() string { return a.services.ReadClipboard() }

// setClipboard puts text on the clipboard.
func (a *app) setClipboard(s string) { a.services.WriteClipboard(s) }

// view builds the window's interface for this frame.
func (a *app) view(c *ui.Context) {
	if !a.haveServices {
		a.services, a.haveServices = c.Services(), true
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			p := pageByID(a.page)
			ui.Column(c).Padding(22, 26, 12).Gap(2).Children(func() {
				ui.Text(c, p.title).FontSize(22).Bold()
				ui.Text(c, p.blurb).TextColor(c.Theme().TextMuted)
			})
			ui.Column(c).Grow(1).MinHeight(0).Children(func() {
				switch a.page {
				case "download":
					a.downloadPage(c)
				case "batch":
					a.batchPage(c)
				case "playlist":
					a.playlistPage(c)
				case "library":
					a.libraryPage(c)
				case "settings":
					a.settingsPage(c)
				case "logs":
					a.logsPage(c)
				case "about":
					a.aboutPage(c)
				}
			})
		})
	})
	for i, p := range pages {
		if c.Shortcut(ui.Cmd, ui.Key1+ui.Key(i)) {
			a.goTo(p.id)
		}
	}
}

// sidebar is the source list the pages are chosen in.
func (a *app) sidebar(c *ui.Context) {
	if ui.Sidebar(c, &a.page, func() {
		for _, p := range pages {
			item := ui.SidebarItem(c, p.id, pageIcon(p.id), p.title)
			if p.id == "library" {
				item.Children(func() {
					if n := a.mgr.active(); n > 0 {
						ui.Badge(c, fmt.Sprint(n))
					}
				})
			}
		}
	}).Width(214).Changed() {
		a.goTo(a.page)
	}
}

func pageIcon(id string) *ui.SVG {
	switch id {
	case "download":
		return iconDownload
	case "batch":
		return iconBatch
	case "playlist":
		return iconPlaylist
	case "library":
		return iconLibrary
	case "settings":
		return iconSettings
	case "logs":
		return iconLogs
	default:
		return iconAbout
	}
}

// card is a group of controls with a title, as the pages lay themselves out.
func card(c *ui.Context, title string, body func()) ui.Element {
	t := c.Theme()
	return ui.Column(c).Padding(18).Gap(12).Radius(10).Background(t.Surface).
		Border(1, t.Border).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(15).Bold()
		}
		body()
	})
}

// scrollPage is the body of a page that scrolls as a whole.
func scrollPage(c *ui.Context, body func()) {
	ui.Scroll(c).Grow(1).Padding(4, 26, 26).Gap(16).Children(body)
}

// muted is a line of secondary text.
func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

// emptyState is what a page shows when it has nothing to list.
func emptyState(c *ui.Context, ic *ui.SVG, s string) {
	ui.Column(c).Padding(28).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Icon(c, ic).FontSize(30).TextColor(c.Theme().TextMuted)
		muted(c, s)
	})
}

// iconButton is a button showing only an icon, named for assistive
// technology.
func iconButton(c *ui.Context, ic *ui.SVG, label string, fn func()) ui.Element {
	b := ui.Button(c, "").Label(label)
	b.Children(func() { ui.Icon(c, ic).FontSize(16) })
	b.OnClick(fn)
	return b
}

// duration writes seconds as h:mm:ss, as the downloaders do.
func duration(sec float64) string {
	if sec <= 0 {
		return ""
	}
	d := int(sec)
	if d < 3600 {
		return fmt.Sprintf("%d:%02d", d/60, d%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", d/3600, d/60%60, d%60)
}

// splitArgs splits a command line into arguments, honoring single and
// double quotes as a shell does without its expansions.
func splitArgs(s string) []string {
	var (
		out   []string
		cur   strings.Builder
		quote byte
		has   bool
	)
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case quote != 0:
			if ch == quote {
				quote = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '\'' || ch == '"':
			quote, has = ch, true
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteByte(ch)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// splitLines returns the non-empty lines of a text, trimmed, without
// comments.
func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// exists reports whether a path is there.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
