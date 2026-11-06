package main

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// testApp is the app the tests drive, without a window: the view runs on a
// Tester, which clicks and types as a user would. The manager is told to
// run nothing, so no test starts a real download.
func testApp(t *testing.T) (*app, *ui.Tester) {
	t.Helper()
	set := defaultSettings()
	set.Notify, set.KeepAwake = false, false
	a := newApp(set)
	a.mgr.max = 0
	tt := ui.NewTester(a.view, 900, 700)
	return a, tt
}

// TestPages checks that every page builds and that the sidebar moves
// between them.
func TestPages(t *testing.T) {
	a, tt := testApp(t)
	for _, p := range pages {
		a.page = p.id
		tt.Frame()
		if !tt.HasText(p.title) {
			t.Errorf("page %s does not show its title, texts %q", p.id, tt.Texts())
		}
	}
}

// TestDownloadQueues checks that the Download button queues the link with
// the engine and the preset chosen, and shows the library.
func TestDownloadQueues(t *testing.T) {
	a, tt := testApp(t)
	// A link the app does not take for another engine, so the queued
	// download keeps the engine the page shows.
	a.page, a.url, a.preset = "download", "https://www.youtube.com/watch?v=x", "Best (video + audio)"
	tt.Frame()
	if err := tt.Click("Download"); err != nil {
		t.Fatal(err)
	}
	if a.page != "library" {
		t.Errorf("page after a download is %q", a.page)
	}
	tasks := a.mgr.snapshot()
	if len(tasks) != 1 {
		t.Fatalf("%d tasks queued", len(tasks))
	}
	if got := tasks[0].url; got != "https://www.youtube.com/watch?v=x" {
		t.Errorf("queued url %q", got)
	}
	if tasks[0].engineID != a.engine {
		t.Errorf("queued engine %q", tasks[0].engineID)
	}
	if tasks[0].state != statusQueued {
		t.Errorf("queued state %v", tasks[0].state)
	}
}

// TestQueueCarriesOptions checks that what the page shows reaches the
// downloader's command line, and that a second engine is not given the
// first one's extra options.
func TestQueueCarriesOptions(t *testing.T) {
	a, tt := testApp(t)
	a.page, a.extra = "download", "--embed-subs --quiet"
	a.url = "https://www.youtube.com/watch?v=y"
	tt.Frame()
	if err := tt.Click("Download"); err != nil {
		t.Fatal(err)
	}
	tasks := a.mgr.snapshot()
	if len(tasks) != 1 {
		t.Fatalf("%d tasks queued", len(tasks))
	}
	j := tasks[0].job
	if !contains(j.args, "--embed-subs") || !contains(j.args, "--quiet") {
		t.Errorf("command %v is missing the extra options", j.args)
	}
	if got := j.args[len(j.args)-1]; got != tasks[0].url {
		t.Errorf("command %v does not end with the url", j.args)
	}
}

// TestPagesEmbedSubtitles checks that every page which queues a download
// offers the subtitle choice, and that clicking it reaches the setting the
// command is made from.
func TestPagesEmbedSubtitles(t *testing.T) {
	for _, page := range []string{"download", "batch", "playlist"} {
		a, tt := testApp(t)
		a.page = page
		a.url, a.batchURL, a.listURL = "https://www.youtube.com/watch?v=x", "https://youtu.be/x", "https://youtu.be/x"
		tt.Frame()
		if !tt.HasText("Embed the subtitles in the video") {
			t.Errorf("the %s page offers no subtitle choice", page)
			continue
		}
		if err := tt.Click("Embed the subtitles in the video"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !a.set.EmbedSubs {
			t.Errorf("the %s page did not reach the setting", page)
		}
	}
}

// TestSubtitlesPage checks that the Subtitles page queues a link's subtitles
// alone, rewritten where they are written, whatever engine the other pages
// use.
func TestSubtitlesPage(t *testing.T) {
	a, tt := testApp(t)
	a.setEngine("gallery-dl")
	a.page, a.subURL = "subtitles", "https://www.youtube.com/playlist?list=PLx"
	tt.Frame()
	if err := tt.Click("Download subtitles"); err != nil {
		t.Fatal(err)
	}
	tasks := a.mgr.snapshot()
	if len(tasks) != 1 {
		t.Fatalf("%d tasks queued", len(tasks))
	}
	tk := tasks[0]
	if tk.engineID != "yt-dlp" {
		t.Errorf("subtitles are fetched by %q", tk.engineID)
	}
	if tk.job.embedSubs || !tk.job.cleanSubs {
		t.Errorf("job embeds %v, rewrites %v", tk.job.embedSubs, tk.job.cleanSubs)
	}
	for _, want := range []string{"--skip-download", "--write-subs", "--sub-format"} {
		if !contains(tk.job.args, want) {
			t.Errorf("command %v is missing %q", tk.job.args, want)
		}
	}
	if a.page != "library" {
		t.Errorf("page after asking for subtitles is %q", a.page)
	}
}

// TestEngineFromLink checks the guess the app makes from a link's host.
func TestEngineFromLink(t *testing.T) {
	for url, want := range map[string]string{
		"https://www.youtube.com/watch?v=x":       "yt-dlp",
		"https://svtplay.se/video/x":              "svtplay-dl",
		"https://www.bilibili.com/video/BV1xx":    "lux",
		"https://imgur.com/gallery/abc":           "gallery-dl",
		"https://v.qq.com/x/cover/x.html":         "you-get",
		"https://example.com/file.zip":            "aria2c",
		"magnet:?xt=urn:btih:abc":                 "aria2c",
		"https://unknown.example/watch?v=1":       "yt-dlp",
		"https://www.reddit.com/r/x/comments/1/a": "gallery-dl",
	} {
		if got := engineFor(url); got != want {
			t.Errorf("engineFor(%q) = %q, want %q", url, got, want)
		}
	}
}

// TestEngineSetRemembersPreset checks that choosing an engine takes the
// preset last used with it.
func TestEngineSetRemembersPreset(t *testing.T) {
	a, _ := testApp(t)
	a.setEngine("gallery-dl")
	a.setPreset("Metadata only (JSON)")
	a.setEngine("yt-dlp")
	if a.preset != engineByID("yt-dlp").presets[0].name {
		t.Errorf("preset after switching to yt-dlp is %q", a.preset)
	}
	a.setEngine("gallery-dl")
	if a.preset != "Metadata only (JSON)" {
		t.Errorf("preset after switching back is %q", a.preset)
	}
}

// TestCommand checks the command line a task runs.
func TestCommand(t *testing.T) {
	a, _ := testApp(t)
	a.set.Template = "%(title)s.%(ext)s"
	a.set.Cookies = "/tmp/cookies.txt"
	a.extra = "--embed-subs"
	j := a.command("https://x", "yt-dlp", "Best", []string{"-f", "bv*+ba/b"})
	if j.bin != "yt-dlp" {
		t.Errorf("binary %q", j.bin)
	}
	joined := strings.Join(j.args, " ")
	for _, want := range []string{"-f bv*+ba/b", "-o %(title)s.%(ext)s", "--newline", "--cookies /tmp/cookies.txt", "--embed-subs"} {
		if !strings.Contains(joined, want) {
			t.Errorf("command %q is missing %q", joined, want)
		}
	}
	if !strings.HasSuffix(joined, "https://x") {
		t.Errorf("command %q does not end with the url", joined)
	}
	// A resumed download continues the partial file.
	if j := a.command("https://x", "yt-dlp", "Best", nil); j.resumeFlag != "-c" {
		t.Errorf("yt-dlp does not continue a partial file: %q", j.resumeFlag)
	}
	// Subtitles are written beside the video by an engine that can, and
	// muxed in by the app, so only when the setting asks for them.
	a.extra, a.set.EmbedSubs, a.set.SubLangs = "", true, "en,ja"
	j = a.command("https://x", "yt-dlp", "Best", nil)
	if !j.embedSubs {
		t.Error("yt-dlp is asked for the subtitles but the job does not embed them")
	}
	joined = strings.Join(j.args, " ")
	// The native srt of the engine, or the words repeat.
	for _, want := range []string{"--write-subs", "--sub-langs en,ja", "--sub-format srt/vtt/best"} {
		if !strings.Contains(joined, want) {
			t.Errorf("command %q is missing %q", joined, want)
		}
	}
	// An audio download asks for no subtitles it cannot embed.
	for _, want := range []string{"--write-subs", "--sub-langs"} {
		if j := a.command("https://x", "yt-dlp", "Audio: MP3", []string{"-x", "--audio-format", "mp3"}); contains(j.args, want) || j.embedSubs {
			t.Errorf("audio command %v has %q", j.args, want)
		}
	}
	// Nor does one that downloads no video: it rewrites them where they
	// are written instead of embedding them.
	j = a.command("https://x", "yt-dlp", "Subtitles only", presetArgs("yt-dlp", "Subtitles only"))
	if j.embedSubs {
		t.Errorf("subtitles-only command %v embeds what it did not download", j.args)
	}
	if !j.cleanSubs {
		t.Error("a subtitles-only download does not rewrite its subtitles")
	}
	// wget takes its own flags for the same settings.
	if j := a.command("https://x", "wget", "Download", nil); contains(j.args, "-o") || j.resumeFlag != "-c" {
		t.Errorf("wget command %v", j.args)
	}
	if j := a.command("https://x", "wget", "Download", nil); contains(j.args, "--write-subs") {
		t.Errorf("wget command %v got a subtitle flag it does not take", j.args)
	}
}

// TestSplitArgs checks the shell-like splitting of extra options.
func TestSplitArgs(t *testing.T) {
	for in, want := range map[string][]string{
		"":                      nil,
		"  --embed-subs  ":      {"--embed-subs"},
		`--ppa "ffmpeg:-c:v x"`: {"--ppa", "ffmpeg:-c:v x"},
		`-f 'bv*+ba' --quiet`:   {"-f", "bv*+ba", "--quiet"},
	} {
		got := splitArgs(in)
		if len(got) != len(want) {
			t.Errorf("splitArgs(%q) = %q, want %q", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitArgs(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

// TestProgress folds the lines the downloaders print into a task.
func TestProgress(t *testing.T) {
	m := &manager{}
	tk := &task{percent: -1}
	for _, line := range []string{
		"[youtube] dQw4w9WgXcQ: Downloading webpage",
		`[download] Destination: Never Gonna Give You Up.f248.webm`,
		"[download]  45.2% of  12.34MiB at  1.23MiB/s ETA 00:08",
		`[download] 100% of 12.34MiB in 00:10`,
	} {
		m.fold(tk, line)
	}
	if tk.percent != 100 {
		t.Errorf("percent %v", tk.percent)
	}
	if tk.speed != "1.23 MiB/s" {
		t.Errorf("speed %q", tk.speed)
	}
	if tk.eta != "00:08" {
		t.Errorf("eta %q", tk.eta)
	}
	if tk.size != "12.34MiB" {
		t.Errorf("size %q", tk.size)
	}
	if tk.file != "Never Gonna Give You Up.f248.webm" {
		t.Errorf("file %q", tk.file)
	}
	if tk.title != "Never Gonna Give You Up.f248" {
		t.Errorf("title %q", tk.title)
	}
	// wget names the file its own way, and ends with "saved [n]".
	tk = &task{percent: -1}
	for _, line := range []string{
		"Saving to: ‘report.pdf’",
		"100%[===================>]  12.35M  --.-KB/s    in 0.1s",
		"‘report.pdf’ saved [12952431]",
	} {
		m.fold(tk, line)
	}
	if tk.file != "report.pdf" || tk.title != "report" {
		t.Errorf("wget file %q title %q", tk.file, tk.title)
	}
	if tk.percent != 100 {
		t.Errorf("wget percent %v", tk.percent)
	}
}

// TestWgetProgress folds what wget really prints. Its progress bar carries
// no total and its speed only appears in the summary line, so the size has
// to come from the Length line it prints once before it starts.
func TestWgetProgress(t *testing.T) {
	m := &manager{}
	tk := &task{percent: -1}
	for _, line := range []string{
		"--2026-11-06 22:15:45--  https://example.com/f.bin",
		"Resolving example.com... 1.2.3.4",
		"Connecting to example.com|1.2.3.4|:443... connected.",
		"HTTP request sent, awaiting response... 200 OK",
		"Length: 179127 (175K) [text/plain]",
		"Saving to: \u2018report.pdf\u2019",
		"     0K .......... .......... .......... .......... .......... 28%  656K 0s",
		"   150K .......... .......... ....                            100% 1.22M=0.2s",
		"2026-11-06 22:15:45 (1.05 MB/s) - \u2018report.pdf\u2019 saved [179127/179127]",
	} {
		m.fold(tk, line)
	}
	if tk.percent != 100 {
		t.Errorf("percent %v", tk.percent)
	}
	if tk.file != "report.pdf" || tk.title != "report" {
		t.Errorf("file %q title %q", tk.file, tk.title)
	}
	if tk.size != "175K (179127 B)" {
		t.Errorf("size %q", tk.size)
	}
	if tk.speed != "1.05 MB/s" {
		t.Errorf("speed %q", tk.speed)
	}
}

// TestYtDlpProgress folds what yt-dlp really prints for a merged download,
// so the parser is checked against the tool and not against itself.
func TestYtDlpProgress(t *testing.T) {
	m := &manager{}
	tk := &task{percent: -1}
	for _, line := range []string{
		"[youtube] dQw4w9WgXcQ: Downloading webpage",
		"[info] dQw4w9WgXcQ: Downloading 1 format(s): 140",
		"[download] Destination: A Video [dQw4w9WgXcQ].m4a",
		"[download]   0.0% of    3.29MiB at  Unknown B/s ETA Unknown",
		"[download]  60.8% of    3.29MiB at   4.38MiB/s ETA 00:00",
		"[download] 100% of    3.29MiB in 00:00:01 at 2.42MiB/s",
		"[FixupM4a] Correcting container of \"A Video [dQw4w9WgXcQ].m4a\"",
	} {
		m.fold(tk, line)
	}
	if tk.percent != 100 {
		t.Errorf("percent %v", tk.percent)
	}
	if tk.file != "A Video [dQw4w9WgXcQ].m4a" {
		t.Errorf("file %q", tk.file)
	}
	if tk.title != "A Video [dQw4w9WgXcQ]" {
		t.Errorf("title %q", tk.title)
	}
	if tk.size != "3.29MiB" {
		t.Errorf("size %q", tk.size)
	}
	// The unknown speed and ETA of the first line must not be kept as the
	// download's own.
	if tk.speed == "Unknown B/s" || tk.eta == "Unknown" {
		t.Errorf("speed %q eta %q", tk.speed, tk.eta)
	}
}

// TestParseInfo reads what an engine prints for -J.
func TestParseInfo(t *testing.T) {
	info, err := parseInfo([]byte(`{
		"id": "abc", "title": "A video", "duration": 95,
		"formats": [
			{"format_id":"137","ext":"mp4","width":1920,"height":1080,"filesize":12345678,"vcodec":"avc1","acodec":"none","format_note":"1080p"},
			{"format_id":"140","ext":"m4a","filesize_approx":1000,"vcodec":"none","acodec":"mp4a"}
		]}`), false, "")
	if err != nil {
		t.Fatal(err)
	}
	if info.title != "A video" || len(info.formats) != 2 {
		t.Fatalf("info %+v", info)
	}
	if f := info.formats[0]; !f.video || f.audio || f.size != 12345678 || resolution(f) != "1920×1080" {
		t.Errorf("first format %+v", f)
	}
	if f := info.formats[1]; f.video || !f.audio || f.size != 1000 {
		t.Errorf("second format %+v", f)
	}
	// A storyboard has neither codec: it is not a stream, and choosing one
	// would download nothing playable.
	storyboard, err := parseInfo([]byte(`{"title":"t","formats":[
		{"format_id":"sb0","ext":"mhtml","vcodec":"none","acodec":"none","format_note":"storyboard"},
		{"format_id":"sb3","ext":"mhtml","width":48,"height":27,"vcodec":"none","acodec":"none"},
		{"format_id":"18","ext":"mp4","vcodec":"avc1","acodec":"mp4a"}]}`), false, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(storyboard.formats) != 1 || storyboard.formats[0].id != "18" {
		t.Errorf("storyboards were not filtered out: %+v", storyboard.formats)
	}
	if got := formatNote(info.formats[0]); !strings.Contains(got, "video only") {
		t.Errorf("note %q", got)
	}

	entries, err := parseInfo([]byte(`{"title":"A list","entries":[
		{"id":"1","title":"One","url":"https://x/1","duration":61},
		{"id":"2","title":"Two","webpage_url":"https://x/2"},
		{"id":"3","title":"Three","url":"/watch?v=rel"}]}`), true, "https://www.youtube.com/playlist?list=PL1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries.entries) != 3 {
		t.Fatalf("entries %+v", entries.entries)
	}
	if entries.entries[1].url != "https://x/2" {
		t.Errorf("entry url %q", entries.entries[1].url)
	}
	// A flat playlist gives an entry a link relative to the site, which a
	// downloader cannot fetch on its own.
	if got := entries.entries[2].url; got != "https://www.youtube.com/watch?v=rel" {
		t.Errorf("relative entry url %q", got)
	}
	if duration(61) != "1:01" || duration(3661) != "1:01:01" {
		t.Errorf("durations %q %q", duration(61), duration(3661))
	}
}

// TestBatchQueuesEveryLink checks the batch list is queued in order.
func TestBatchQueuesEveryLink(t *testing.T) {
	a, tt := testApp(t)
	a.page = "batch"
	a.batch = []string{"https://example.com/a", "https://example.com/b", ""}
	tt.Frame()
	if err := tt.Click("Download 3 link(s)"); err != nil {
		t.Fatal(err)
	}
	if len(a.mgr.snapshot()) != 2 {
		t.Errorf("%d tasks queued from the batch", len(a.mgr.snapshot()))
	}
	if len(a.batch) != 0 {
		t.Errorf("batch list still holds %d links", len(a.batch))
	}
}

// TestLibraryFilters checks the library's filter.
func TestLibraryFilters(t *testing.T) {
	a, tt := testApp(t)
	a.page = "library"
	a.mgr.add(&task{url: "https://example.com/done", engineID: "yt-dlp", preset: "Best"})
	a.mgr.add(&task{url: "https://example.com/waiting", engineID: "yt-dlp", preset: "Best"})
	a.mgr.setState(a.mgr.snapshot()[0].id, statusDone)
	tt.Frame()
	a.filter = 2
	if n := len(a.filtered()); n != 1 {
		t.Errorf("completed filter shows %d", n)
	}
	a.filter = 1
	if n := len(a.filtered()); n != 1 {
		t.Errorf("active filter shows %d", n)
	}
	if err := tt.Click("Completed"); err != nil {
		t.Fatal(err)
	}
	if a.filter != 2 {
		t.Errorf("filter after a click is %d", a.filter)
	}
}

// TestSplitLines reads a file of links.
func TestSplitLines(t *testing.T) {
	got := splitLines("# a comment\nhttps://example.com/a\n\n  https://example.com/b  \n")
	if len(got) != 2 || got[0] != "https://example.com/a" || got[1] != "https://example.com/b" {
		t.Errorf("splitLines = %q", got)
	}
}

// TestHumanSize writes sizes the way the downloaders do.
func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0 B", 999: "999 B", 1024: "1.0 KiB", 1536: "1.5 KiB",
		1048576: "1.0 MiB", 1073741824: "1.0 GiB",
	} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestEnginesAreUsable checks every engine has a name, a binary and at
// least one preset.
func TestEnginesAreUsable(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range engines {
		if e.id == "" || e.name == "" || e.bin == "" || len(e.presets) == 0 {
			t.Errorf("engine %+v is incomplete", e)
		}
		if seen[e.id] {
			t.Errorf("engine id %q twice", e.id)
		}
		seen[e.id] = true
		if engineByID(e.id).id != e.id {
			t.Errorf("engineByID(%q) = %q", e.id, engineByID(e.id).id)
		}
		if len(engineNames()) != len(engines) {
			t.Errorf("engineNames has %d names for %d engines", len(engineNames()), len(engines))
		}
		if len(presetNames(e.id)) != len(e.presets) {
			t.Errorf("engine %s has %d names for %d presets", e.id, len(presetNames(e.id)), len(e.presets))
		}
	}
}

// contains reports whether args holds s.
func contains(args []string, s string) bool {
	for _, a := range args {
		if a == s {
			return true
		}
	}
	return false
}
