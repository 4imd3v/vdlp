package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/slog"
)

// waitCutoff is how long a finished download's output is given to be read
// after the command has been reaped. A downloader's child, ffmpeg merging
// formats say, can outlive it and hold the pipe open; without this the task
// would wait for output that never comes.
const waitCutoff = 5 * time.Second

// status is where a download stands.
type status int

const (
	statusQueued status = iota
	statusRunning
	statusPaused
	statusDone
	statusFailed
)

func (s status) String() string {
	switch s {
	case statusQueued:
		return "Queued"
	case statusRunning:
		return "Downloading"
	case statusPaused:
		return "Paused"
	case statusDone:
		return "Completed"
	case statusFailed:
		return "Failed"
	}
	return ""
}

// stopped reports whether the download is over, well or badly.
func (s status) stopped() bool {
	return s == statusDone || s == statusFailed
}

// job is everything one download runs with, read from the settings once
// as it is queued: the view may change them while it runs.
type job struct {
	bin        string
	args       []string
	dir        string
	proxy      string
	resumeFlag string
	// embedSubs asks for the subtitles the engine writes to be rewritten
	// and muxed into the video once it has downloaded it.
	embedSubs bool
}

// task is one download: a link, the engine that fetches it and how it goes.
type task struct {
	id       int64
	url      string
	engineID string
	preset   string
	// job is what the download runs, made when it is queued.
	job job
	// title is what the library shows; it starts as the link and takes the
	// file's name once the downloader says where it wrote.
	title string
	state status
	// percent is 0..100, or -1 while the length is unknown.
	percent float64
	speed   string
	eta     string
	size    string
	// file is where the downloader said it wrote, once known.
	file string
	err  string
	// resume asks the next run to continue a partial file.
	resume  bool
	addedAt time.Time
	endedAt time.Time
	// log is the tail of the downloader's output, for the details dialog.
	log []string

	// cancel stops the downloader running this task, and is nil when
	// nothing runs it.
	cancel context.CancelFunc
}

// path is where the downloaded file is, once known.
func (t *task) path() string {
	if t.file == "" {
		return ""
	}
	if filepath.IsAbs(t.file) {
		return t.file
	}
	return filepath.Join(t.job.dir, t.file)
}

// The downloaders each write "12.3% of 45MiB at 1.2MiB/s ETA 00:10" their
// own way; these patterns take what they all share.
var (
	rePercent = regexp.MustCompile(`(\d+(?:\.\d+)?)%`)
	reSpeed   = regexp.MustCompile(`([\d.]+)\s*([KMGT]?i?B/s)`)
	reETA     = regexp.MustCompile(`ETA[:\s]+([0-9hms:]+)`)
	reTotal   = regexp.MustCompile(`of\s+~?\s*([\d.]+\s*[KMGT]?i?B)`)
	// wget prints the length of what it is about to fetch once, before it
	// starts, and never again.
	reLength = regexp.MustCompile(`^Length:\s*(\d+)\s*\(([^)]+)\)`)
	reDest   = regexp.MustCompile(`(?i)(?:Destination|Merging formats into)\s*:?\s*"?(.+?)"?\s*$`)
	reSave   = regexp.MustCompile(`Saving to: [‘'](.+)[’']`)
	reSaved  = regexp.MustCompile(`saved \[(\d+)\]`)
)

// manager runs the downloads, a few at a time, and keeps their state for
// the library. Its methods are safe from any goroutine; the view reads a
// task's fields on the main thread only, once the manager has published
// the change through Window.Update.
type manager struct {
	mu     sync.Mutex
	tasks  []*task
	nextID int64
	// max is how many downloads run at once.
	max int
	// command builds what a download runs with, from the settings. It runs
	// on the UI thread; the manager itself only carries what it made.
	command func(url, engineID, preset string, args []string) job
	// publish asks the window for a frame after a change.
	publish func()
	// awake holds the computer awake while downloads run.
	awake func() func()
	// notify reports a finished download.
	notify func(task)
	// logger is where the manager reports what it did and what went wrong.
	logger *slog.Logger

	running      int
	awakeHeld    bool
	releaseAwake func()
	stopped      bool
}

func newManager(max int, command func(url, engineID, preset string, args []string) job) *manager {
	if max < 1 {
		max = 1
	}
	return &manager{max: max, command: command}
}

// setMax changes how many downloads run at once and starts those waiting
// for room.
func (m *manager) setMax(n int) {
	if n < 1 {
		n = 1
	}
	m.mu.Lock()
	m.max = n
	m.mu.Unlock()
	m.startNext()
}

// log returns the slog logger the manager logs through.
func (m *manager) log() *slog.Logger {
	if m.logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.logger
}

// abandon takes a task out of the running set after a panic, so the queue
// does not stall on a download that will never report back. It does nothing
// unless the task is still running, so it is safe to call after finish has
// already run.
func (m *manager) abandon(t *task, err error) {
	m.mu.Lock()
	if t.state == statusRunning {
		m.running--
		t.state, t.err = statusFailed, err.Error()
		t.endedAt = time.Now()
	}
	m.mu.Unlock()
	m.startNext()
	m.publish()
}

// add queues a download and returns it. Its job must already be made.
func (m *manager) add(t *task) *task {
	m.mu.Lock()
	m.nextID++
	t.id, t.title, t.percent, t.addedAt = m.nextID, t.url, -1, time.Now()
	m.tasks = append(m.tasks, t)
	m.mu.Unlock()
	m.startNext()
	return t
}

// snapshot copies the tasks for the view to build from. They are copies,
// not the tasks themselves: the view reads them on the UI thread while the
// downloads change the real ones on theirs.
func (m *manager) snapshot() []task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]task, len(m.tasks))
	for i, t := range m.tasks {
		out[i] = *t
	}
	return out
}

// get returns a copy of the task with that id, and whether there is one.
func (m *manager) get(id int64) (task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.id == id {
			return *t, true
		}
	}
	return task{}, false
}

// active reports how many downloads are under way.
func (m *manager) active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// remove takes a task out of the library, stopping it first.
func (m *manager) remove(id int64) {
	m.mu.Lock()
	for i, t := range m.tasks {
		if t.id == id {
			m.cancelLocked(t)
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	m.publish()
}

// clearFinished drops every task that has stopped.
func (m *manager) clearFinished() {
	m.mu.Lock()
	kept := m.tasks[:0]
	for _, t := range m.tasks {
		if t.state.stopped() {
			continue
		}
		kept = append(kept, t)
	}
	m.tasks = kept
	m.mu.Unlock()
	m.publish()
}

// pause stops a download and holds it, so the next run continues the
// partial file: the downloaders resume with -c, which is how a stopped
// download picks up where it left off on every platform.
func (m *manager) pause(id int64) {
	m.mu.Lock()
	for _, t := range m.tasks {
		if t.id != id || (t.state != statusRunning && t.state != statusQueued) {
			continue
		}
		t.resume = true
		m.cancelLocked(t)
		t.state = statusPaused
		t.percent, t.speed, t.eta, t.size = -1, "", "", ""
	}
	m.mu.Unlock()
	m.startNext()
	m.publish()
}

// resume queues a paused, failed download again.
func (m *manager) resume(id int64) {
	m.mu.Lock()
	for _, t := range m.tasks {
		if t.id != id || (t.state != statusPaused && !t.state.stopped()) {
			continue
		}
		m.cancelLocked(t)
		t.state, t.err = statusQueued, ""
		t.resume = true
		t.percent, t.speed, t.eta, t.size = -1, "", "", ""
	}
	m.mu.Unlock()
	m.startNext()
	m.publish()
}

// retry runs a download again from the start.
func (m *manager) retry(id int64) {
	m.mu.Lock()
	for _, t := range m.tasks {
		if t.id != id {
			continue
		}
		// A download still running is stopped first, so one task never
		// has two commands writing to the same folder.
		m.cancelLocked(t)
		t.state, t.err = statusQueued, ""
		t.resume = false
		t.percent, t.speed, t.eta, t.size = -1, "", "", ""
		t.endedAt = time.Time{}
	}
	m.mu.Unlock()
	m.startNext()
	m.publish()
}

// cancelLocked stops t's command if it has one. The caller holds the mutex.
func (m *manager) cancelLocked(t *task) {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// startNext starts as many queued downloads as there is room for.
func (m *manager) startNext() {
	m.mu.Lock()
	// running is what a started download needs to go: its task, and the
	// context that stops it.
	type running struct {
		t      *task
		ctx    context.Context
		cancel context.CancelFunc
	}
	var start []running
	for _, t := range m.tasks {
		if m.running >= m.max || m.stopped {
			break
		}
		if t.state != statusQueued {
			continue
		}
		// The way to stop the download is in place, on the task, before
		// the task is said to be running, so that a pause arriving with
		// the very first frame reaches it. It is also handed to the
		// goroutine, which must not read it off the task afterwards.
		ctx, cancel := context.WithCancel(context.Background())
		t.state, t.cancel = statusRunning, cancel
		m.running++
		start = append(start, running{t, ctx, cancel})
	}
	m.mu.Unlock()
	for _, r := range start {
		go m.run(r.t, r.ctx, r.cancel)
	}
	m.keepAwake()
}

// keepAwake holds the computer awake while anything downloads.
func (m *manager) keepAwake() {
	if m.awake == nil {
		return
	}
	m.mu.Lock()
	n := m.running
	m.mu.Unlock()
	switch {
	case n > 0 && !m.awakeHeld:
		m.awakeHeld = true
		m.releaseAwake = m.awake()
	case n == 0 && m.awakeHeld:
		m.awakeHeld = false
		if m.releaseAwake != nil {
			m.releaseAwake()
			m.releaseAwake = nil
		}
	}
}

// run executes one download and folds its output into the task. ctx stops
// it, and cancel releases it as run returns.
func (m *manager) run(t *task, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	// A panic here would take the process down with only a stack on the
	// terminal: log it, and leave the queue able to carry on.
	defer func() {
		if r := recover(); r != nil {
			m.log().Error("panic in a download", "url", t.url, "panic", fmt.Sprint(r),
				"stack", string(debug.Stack()))
			m.abandon(t, fmt.Errorf("the app failed while running this download: %v", r))
		}
	}()
	// What the download runs with is read once, under the mutex: the view
	// may change the settings, or pause this download, while it goes.
	m.mu.Lock()
	j, resume := t.job, t.resume
	m.mu.Unlock()
	if resume && j.resumeFlag != "" {
		j.args = append(slices.Clone(j.args), j.resumeFlag)
	}
	cmd := exec.CommandContext(ctx, j.bin, j.args...)
	// A downloader's child, ffmpeg merging formats say, can outlive it and
	// hold the output pipe open. WaitDelay bounds how long Wait waits for
	// the output to end after the command itself has exited.
	cmd.WaitDelay = 2 * time.Second
	cmd.Dir = j.dir
	if j.proxy != "" {
		cmd.Env = append(os.Environ(),
			"HTTP_PROXY="+j.proxy, "http_proxy="+j.proxy,
			"HTTPS_PROXY="+j.proxy, "https_proxy="+j.proxy,
			"ALL_PROXY="+j.proxy, "all_proxy="+j.proxy)
	} else {
		cmd.Env = os.Environ()
	}
	// The downloaders write their progress to both streams. The pipe is the
	// app's own, not the command's: StdoutPipe hands its read end to Wait,
	// which closes it as soon as the command exits and would cut the output
	// short. Holding both ends here, the read runs to the end of the output
	// and only then is the command reaped.
	read, write, err := os.Pipe()
	if err != nil {
		m.failed(t, j, err)
		return
	}
	defer read.Close()
	cmd.Stdout, cmd.Stderr = write, write

	if err := cmd.Start(); err != nil {
		write.Close()
		m.failed(t, j, fmt.Errorf("%s: %w", j.bin, err))
		return
	}
	// The command holds the write end now; ours is closed so the read sees
	// the end of the output when the command and its children are done.
	write.Close()
	// The command is reaped on a goroutine of its own, and the output is
	// cut short when it ends: reading to the end of the pipe first would
	// leave a task waiting on a child that outlived its downloader.
	m.log().Info("download started", "url", t.url, "bin", j.bin, "args", j.args, "dir", j.dir)

	// The output is read to its end before the command is reaped. A child
	// that outlived its downloader can hold the write end open, so the read
	// is given a bound: past it, the read end is closed under the reader and
	// the output gathered so far is what the task keeps.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	m.scan(t, read)
	select {
	case err = <-done:
	case <-time.After(waitCutoff):
		read.Close()
		err = <-done
	}
	if err != nil {
		m.failed(t, j, err)
		return
	}
	if j.embedSubs {
		if err := m.embedSubtitles(ctx, t); err != nil {
			m.failed(t, j, err)
			return
		}
	}
	m.finish(t, nil)
}

// embedSubtitles rewrites the subtitles the engine wrote into blocks of the
// two lines a player draws, and muxes them into the video with ffmpeg, which
// the download needed anyway.
func (m *manager) embedSubtitles(ctx context.Context, t *task) error {
	m.mu.Lock()
	video := t.path()
	m.mu.Unlock()
	switch strings.ToLower(filepath.Ext(video)) {
	case ".srt", ".vtt", ".ass", ".ssa", ".ttml", "":
		// A downloader names its subtitles before the video, so the last
		// file it named may be one: they are left beside the video, where
		// a player finds them, rather than failed for nothing.
		m.log().Warn("no video to embed the subtitles in", "url", t.url, "file", video)
		return nil
	}
	subs := subtitleFiles(video)
	if len(subs) == 0 {
		m.log().Warn("no subtitles to embed", "url", t.url, "video", video)
		return nil
	}
	for _, s := range subs {
		if !strings.EqualFold(filepath.Ext(s), ".srt") {
			continue
		}
		b, err := os.ReadFile(s)
		if err != nil {
			return err
		}
		if err := os.WriteFile(s, []byte(cleanSRT(string(b))), 0o644); err != nil {
			return err
		}
	}
	out, err := embedSubtitles(ctx, video, subs)
	if err != nil {
		return err
	}
	for _, s := range subs {
		os.Remove(s)
	}
	m.mu.Lock()
	if out != video {
		// The container had to change for the subtitles to fit.
		t.file = out
	}
	m.mu.Unlock()
	m.log().Info("subtitles embedded", "url", t.url, "file", out, "tracks", len(subs))
	return nil
}

// failed logs why a download could not run, and reports it to the task. The
// downloader's own last words say more than its exit status, so they are
// logged with it.
func (m *manager) failed(t *task, j job, err error) {
	m.mu.Lock()
	last := ""
	if n := len(t.log); n > 0 {
		last = t.log[n-1]
	}
	m.mu.Unlock()
	m.log().Error("download failed", "url", t.url, "bin", j.bin, "args", j.args,
		"dir", j.dir, "err", err, "lastOutput", last)
	m.finish(t, err)
}

// scan reads the downloader's output a line at a time and folds each line
// into the task, publishing a few times a second at most.
func (m *manager) scan(t *task, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	last := time.Time{}
	for sc.Scan() {
		line := sc.Text()
		m.mu.Lock()
		m.fold(t, line)
		m.mu.Unlock()
		if time.Since(last) > 100*time.Millisecond {
			last = time.Now()
			m.publish()
		}
	}
	m.publish()
}

// fold puts one line of a downloader's output into t. The caller holds the
// mutex.
func (m *manager) fold(t *task, line string) {
	t.log = append(t.log, line)
	if len(t.log) > 200 {
		t.log = t.log[len(t.log)-200:]
	}
	if g := reDest.FindStringSubmatch(line); g != nil {
		if name := strings.Trim(strings.TrimSpace(g[1]), `"`); name != "" {
			t.file, t.title = name, trimExt(name)
		}
	}
	if g := reSave.FindStringSubmatch(line); g != nil {
		t.file, t.title = strings.TrimSpace(g[1]), trimExt(strings.TrimSpace(g[1]))
	}
	if g := rePercent.FindStringSubmatch(line); g != nil {
		if v, err := strconv.ParseFloat(g[1], 64); err == nil {
			t.percent = v
		}
	}
	if g := reSpeed.FindStringSubmatch(line); g != nil {
		t.speed = g[1] + " " + g[2]
	}
	if g := reETA.FindStringSubmatch(line); g != nil {
		t.eta = g[1]
	}
	if g := reTotal.FindStringSubmatch(line); g != nil {
		t.size = strings.TrimSpace(g[1])
	}
	if g := reLength.FindStringSubmatch(line); g != nil {
		t.size = g[2] + " (" + g[1] + " B)"
	}
	if reSaved.MatchString(line) {
		t.percent = 100
	}
}

// finish records how a download ended and starts the next one.
func (m *manager) finish(t *task, err error) {
	m.mu.Lock()
	m.running--
	t.cancel = nil
	t.endedAt = time.Now()
	switch {
	case t.state == statusPaused:
		// The run ended because the user stopped it, which is not a
		// failure of the download: it waits to be resumed.
	case err != nil:
		t.state, t.err = statusFailed, err.Error()
		if t.percent < 100 {
			t.percent = -1
		}
	default:
		t.state, t.percent, t.err = statusDone, 100, ""
		if t.file != "" {
			if st, statErr := os.Stat(t.path()); statErr == nil {
				t.size = humanSize(st.Size())
			}
		}
	}
	finished := *t
	m.mu.Unlock()

	m.startNext()
	if finished.state == statusDone {
		m.log().Info("download finished", "url", t.url, "file", t.path(), "size", t.size)
	}
	// A task removed while it ran is not reported: the user took it away.
	if m.notify != nil && m.holds(t) && (finished.state == statusDone || finished.state == statusFailed) {
		m.notify(finished)
	}
	m.publish()
}

// holds reports whether the library still lists t. The caller does not
// hold the mutex.
func (m *manager) holds(t *task) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, other := range m.tasks {
		if other == t {
			return true
		}
	}
	return false
}

// stopAll cancels every running download, as the app quits.
func (m *manager) stopAll() {
	m.mu.Lock()
	m.stopped = true
	for _, t := range m.tasks {
		m.cancelLocked(t)
		if t.state == statusRunning {
			t.state = statusQueued
		}
	}
	m.mu.Unlock()
}

// trimExt takes the extension off a file's name for a display title.
func trimExt(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// humanSize writes a byte count the way the downloaders do.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit && exp < 4; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}
