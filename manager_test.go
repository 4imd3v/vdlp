package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEngine writes a shell script that prints what a downloader prints,
// so the manager can be driven without a network or a real downloader.
func fakeEngine(t *testing.T, name string, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// testManager builds the app the manager tests drive, with a window-less
// publisher: there is no frame to ask for in a test.
func testManager(t *testing.T, dir string, engineID string) *app {
	t.Helper()
	set := defaultSettings()
	set.Notify, set.KeepAwake = false, false
	set.DownloadDir = dir
	a := newApp(set)
	a.mgr.publish = func() {}
	return a
}

// TestManagerRunsADownload drives one download end to end: the engine is a
// script that reports progress and writes a file, and the manager must fold
// its output into the task and report it done.
func TestManagerRunsADownload(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngine(t, "fake-dl", `
echo '[download] Destination: A Video.f137.mp4'
echo '[download]  42.5% of  48.00MiB at  3.10MiB/s ETA 00:24'
echo '[Merger] Merging formats into "A Video.mp4"'
echo '[download] 100% of 48.00MiB in 00:12'
echo done > "A Video.mp4"
exit 0
`)
	a := testManager(t, dir, "yt-dlp")
	a.mgr.setMax(2)

	var published atomic.Int64
	a.mgr.publish = func() { published.Add(1) }
	a.set.Binaries["yt-dlp"] = bin
	a.queue("https://example.com/watch?v=x", "Best", []string{"-f", "bv*+ba/b"})

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if tsk := a.mgr.snapshot()[0]; tsk.state == statusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	tsk := a.mgr.snapshot()[0]
	if tsk.state != statusDone {
		t.Fatalf("task state %v, err %q, log %q", tsk.state, tsk.err, tsk.log)
	}
	if tsk.percent != 100 {
		t.Errorf("percent %v", tsk.percent)
	}
	if tsk.title != "A Video" {
		t.Errorf("title %q", tsk.title)
	}
	if tsk.file != "A Video.mp4" {
		t.Errorf("file %q", tsk.file)
	}
	if p := tsk.path(); !exists(p) {
		t.Errorf("no file at %q", p)
	}
	if tsk.size == "" {
		t.Errorf("size not measured from the file")
	}
	if published.Load() == 0 {
		t.Error("the manager never published a change")
	}
	if !a.mgr.holds(a.mgr.tasks[0]) {
		t.Error("the finished task is no longer in the library")
	}
}

// TestManagerKeepsTheWholeOutput checks that a download's output is read to
// its end. A pipe handed to StdoutPipe is closed by Wait as soon as the
// command exits, which cuts the last lines off and loses the file the
// downloader said it wrote.
func TestManagerKeepsTheWholeOutput(t *testing.T) {
	dir := t.TempDir()
	// Many lines, so that a read cut short is plain to see.
	bin := fakeEngine(t, "fake-chatty", `
echo '[download] Destination: A Video.f137.mp4'
for i in 1 2 3 4 5 6 7 8 9 10; do
  echo "[download]  $((i*10))% of  48.00MiB at  1.00MiB/s ETA 00:01"
done
echo '[Merger] Merging formats into "A Video.mp4"'
echo '[download] 100% of 48.00MiB in 00:12'
echo done > "A Video.mp4"
exit 0
`)
	a := testManager(t, dir, "yt-dlp")
	a.set.Binaries["yt-dlp"] = bin
	a.queue("https://example.com/watch?v=x", "Best", nil)

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if tsk := a.mgr.snapshot()[0]; tsk.state == statusDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tsk := a.mgr.snapshot()[0]
	if tsk.state != statusDone {
		t.Fatalf("state %v, err %q", tsk.state, tsk.err)
	}
	// One destination line, ten progress lines, the merge and the summary.
	if len(tsk.log) < 13 {
		t.Errorf("only %d of the downloader's 13 lines were read: %q", len(tsk.log), tsk.log)
	}
	if last := tsk.log[len(tsk.log)-1]; !strings.Contains(last, "100%") {
		t.Errorf("the last line read is %q, not the completion", last)
	}
	// The merged name is on a line after the one that named the stream, so
	// it is the first thing a short read loses.
	if tsk.file != "A Video.mp4" || tsk.title != "A Video" {
		t.Errorf("file %q title %q", tsk.file, tsk.title)
	}
}

// TestManagerRunsAtMostMax checks that the concurrency limit holds and that
// the queue drains.
func TestManagerRunsAtMostMax(t *testing.T) {
	dir := t.TempDir()
	// Each run takes a moment, so several queued at once overlap unless the
	// limit holds.
	bin := fakeEngine(t, "fake-slow", `
echo '[download]  50% of  10.00MiB at  1.00MiB/s ETA 00:05'
sleep 0.15
exit 0
`)
	a := testManager(t, dir, "yt-dlp")
	a.mgr.setMax(2)
	a.set.Binaries["yt-dlp"] = bin

	for i := 0; i < 5; i++ {
		a.queue("https://example.com/"+fmt.Sprintf("%c", 'a'+i), "Best", nil)
	}
	// While they run, at most two are under way at once.
	peak := 0
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		running := 0
		for _, tsk := range a.mgr.snapshot() {
			if tsk.state == statusRunning {
				running++
			}
		}
		if running > peak {
			peak = running
		}
		if a.mgr.active() == 0 {
			done := true
			for _, tsk := range a.mgr.snapshot() {
				if tsk.state != statusDone {
					done = false
				}
			}
			if done {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if peak > 2 {
		t.Errorf("%d downloads ran at once, the limit is 2", peak)
	}
	for _, tsk := range a.mgr.snapshot() {
		if tsk.state != statusDone {
			t.Errorf("task %q ended %v (%s)", tsk.url, tsk.state, tsk.err)
		}
	}
}

// TestManagerPauseStopsAndResumes checks that pausing stops the downloader
// and queues the task again.
func TestManagerPauseStopsAndResumes(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngine(t, "fake-forever", `
echo '[download]  10% of  10.00MiB at  1.00MiB/s ETA 00:09'
sleep 30
`)
	a := testManager(t, dir, "yt-dlp")
	a.set.Binaries["yt-dlp"] = bin
	a.queue("https://example.com/x", "Best", nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.mgr.snapshot()[0].state == statusRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	a.mgr.pause(a.mgr.snapshot()[0].id)

	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.mgr.active() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tsk := a.mgr.snapshot()[0]
	if tsk.state != statusPaused {
		t.Errorf("state after a pause is %v", tsk.state)
	}
	if !tsk.resume {
		t.Error("a paused download does not ask to continue its partial file")
	}
	if a.mgr.active() != 0 {
		t.Errorf("%d downloads still run after a pause", a.mgr.active())
	}
	// The next run continues the partial file.
	if tsk.job.resumeFlag != "-c" {
		t.Errorf("the next run does not continue the partial file: %q", tsk.job.resumeFlag)
	}
	// Resuming queues it again, and it runs once more.
	a.mgr.resume(tsk.id)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.mgr.snapshot()[0].state == statusRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := a.mgr.snapshot()[0].state; got != statusRunning {
		t.Errorf("state after a resume is %v", got)
	}
	a.mgr.remove(tsk.id)
}

// TestManagerRemoveDropsARunningTask checks that a download taken out of
// the library stops and is not reported.
func TestManagerRemoveDropsARunningTask(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngine(t, "fake-forever", "sleep 30\n")
	a := testManager(t, dir, "yt-dlp")
	a.set.Binaries["yt-dlp"] = bin
	notified := make(chan string, 4)
	a.mgr.notify = func(t task) { notified <- t.url }
	a.mgr.add(&task{url: "https://example.com/x", engineID: "yt-dlp"})
	id := a.mgr.snapshot()[0].id
	a.mgr.remove(id)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.mgr.active() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(a.mgr.snapshot()) != 0 {
		t.Errorf("the library still holds %d tasks", len(a.mgr.snapshot()))
	}
	select {
	case url := <-notified:
		t.Errorf("a removed download was reported: %q", url)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestManagerStopAll checks that quitting stops every download and queues
// no more.
func TestManagerStopAll(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngine(t, "fake-forever", "sleep 30\n")
	a := testManager(t, dir, "yt-dlp")
	a.set.Binaries["yt-dlp"] = bin
	a.queue("https://example.com/a", "Best", nil)
	a.queue("https://example.com/b", "Best", nil)
	a.mgr.stopAll()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if a.mgr.active() == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, tsk := range a.mgr.snapshot() {
		if tsk.state == statusRunning {
			t.Errorf("task %q still runs after the app quit", tsk.url)
		}
	}
	a.queue("https://example.com/c", "Best", nil)
	time.Sleep(200 * time.Millisecond)
	if a.mgr.active() != 0 {
		t.Error("a download started after the app quit")
	}
}

// TestManagerClearFinished checks that finished downloads leave the
// library and running ones stay.
func TestManagerClearFinished(t *testing.T) {
	a := testManager(t, t.TempDir(), "yt-dlp")
	a.mgr.max = 0
	a.queue("https://example.com/a", "Best", nil)
	a.queue("https://example.com/b", "Best", nil)
	a.queue("https://example.com/c", "Best", nil)
	a.mgr.setState(a.mgr.snapshot()[0].id, statusDone)
	a.mgr.setState(a.mgr.snapshot()[2].id, statusFailed)
	a.mgr.clearFinished()
	left := a.mgr.snapshot()
	if len(left) != 1 || left[0].url != "https://example.com/b" {
		t.Errorf("%d tasks left: %v", len(left), urls(left))
	}
}

// setState puts a task in a state, as a finished download would, without
// running one. Tests only.
func (m *manager) setState(id int64, s status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tasks {
		if t.id == id {
			t.state = s
		}
	}
}

// urls lists the tasks' links, for a failure message.
func urls(tasks []task) string {
	var out []string
	for _, t := range tasks {
		out = append(out, t.url)
	}
	return strings.Join(out, ", ")
}

// TestManagerCleansSubtitles checks that a subtitles-only download rewrites
// every file the engine names, as a playlist spreads them over many, into
// blocks of the two lines a player draws.
func TestManagerCleansSubtitles(t *testing.T) {
	dir := t.TempDir()
	bin := fakeEngine(t, "fake-subs",
		"printf '1\\n00:00:00,000 --> 00:00:03,240\\nfirst line\\n\\n2\\n00:00:01,480 --> 00:00:05,160\\nsecond line\\n\\n' > one.en.srt\n"+
			"cp one.en.srt two.en.srt\n"+
			"echo '[download] Destination: one.en.srt'\n"+
			"echo '[download] Destination: two.en.srt'\n")
	a := testManager(t, dir, "yt-dlp")
	a.set.Binaries["yt-dlp"] = bin
	a.set.SubLangs = "en"
	a.mgr.add(&task{
		url:      "https://example.com/playlist",
		engineID: "yt-dlp",
		preset:   "Subtitles only",
		job:      a.command("https://example.com/playlist", "yt-dlp", "Subtitles only", presetArgs("yt-dlp", "Subtitles only")),
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if tsk := a.mgr.snapshot()[0]; tsk.state == statusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tsk := a.mgr.snapshot()[0]; tsk.state != statusDone {
		t.Fatalf("task state %v, err %q, log %q", tsk.state, tsk.err, tsk.log)
	}
	for _, name := range []string{"one.en.srt", "two.en.srt"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		// One block: a number, a timing and the two lines of it.
		if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 4 {
			t.Errorf("%s after rewriting:\n%s", name, b)
		}
	}
}
