package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// testLog builds a log that writes to a buffer instead of a file.
func testLog() (*appLog, *bytes.Buffer) {
	var buf bytes.Buffer
	return &appLog{handler: &logHandler{out: &buf}}, &buf
}

// TestLogWritesBothWays checks that a record reaches the file as a line of
// JSON and the tail as an entry, with its details in both.
func TestLogWritesBothWays(t *testing.T) {
	lg, buf := testLog()
	lg.logger().Error("download ended badly", "url", "https://x/v", "err", "exit status 1")

	line := strings.TrimSpace(buf.String())
	var obj map[string]any
	if err := json.Unmarshal([]byte(line), &obj); err != nil {
		t.Fatalf("the file did not get a line of JSON: %q", line)
	}
	if obj["msg"] != "download ended badly" || obj["level"] != "ERROR" {
		t.Errorf("file record %v", obj)
	}
	if obj["url"] != "https://x/v" || obj["err"] != "exit status 1" {
		t.Errorf("file record is missing its details: %v", obj)
	}

	tail := lg.tail()
	if len(tail) != 1 {
		t.Fatalf("%d entries in the tail", len(tail))
	}
	if tail[0].msg != "download ended badly" || tail[0].level != slog.LevelError {
		t.Errorf("tail entry %+v", tail[0])
	}
	if len(tail[0].attrs) != 2 || !strings.Contains(tail[0].attrs[0], "url=https://x/v") {
		t.Errorf("tail entry attrs %q", tail[0].attrs)
	}
	// The entry reads as one line, for the clipboard.
	if s := tail[0].String(); !strings.Contains(s, "ERROR download ended badly") ||
		!strings.Contains(s, "err=exit status 1") {
		t.Errorf("entry string %q", s)
	}
}

// TestLogTailIsCapped checks that the tail does not grow without end, while
// the file keeps everything.
func TestLogTailIsCapped(t *testing.T) {
	lg, buf := testLog()
	log := lg.logger()
	for i := 0; i < logKeep+50; i++ {
		log.Info("a line")
	}
	if n := len(lg.tail()); n != logKeep {
		t.Errorf("the tail holds %d entries, the cap is %d", n, logKeep)
	}
	if n := strings.Count(buf.String(), "\n"); n != logKeep+50 {
		t.Errorf("the file holds %d lines for %d records", n, logKeep+50)
	}
}

// TestLogFilter checks which entries the Logs page's filter shows.
func TestLogFilter(t *testing.T) {
	entries := []entry{
		{at: time.Now(), level: slog.LevelInfo, msg: "started"},
		{at: time.Now(), level: slog.LevelWarn, msg: "asking failed"},
		{at: time.Now(), level: slog.LevelError, msg: "download failed"},
	}
	a := &app{logFilter: 0}
	if n := len(a.filteredLog(entries)); n != 3 {
		t.Errorf("all shows %d", n)
	}
	a.logFilter = 1
	if n := len(a.filteredLog(entries)); n != 1 {
		t.Errorf("errors shows %d", n)
	}
	a.logFilter = 2
	if n := len(a.filteredLog(entries)); n != 1 {
		t.Errorf("warnings shows %d", n)
	}
	// Nothing of a kind shows the page's empty message, not a blank list.
	a.logFilter = 1
	if n := len(a.filteredLog(entries[:2])); n != 0 {
		t.Errorf("errors of no errors shows %d", n)
	}
}

// TestLogText checks the whole tail as one text, for the clipboard.
func TestLogText(t *testing.T) {
	lg, _ := testLog()
	lg.logger().Info("one")
	lg.logger().Warn("two")
	text := lg.text()
	if strings.Count(text, "\n") != 2 {
		t.Errorf("text %q", text)
	}
	if !strings.Contains(text, "INFO one") || !strings.Contains(text, "WARN two") {
		t.Errorf("text %q", text)
	}
}

// TestPanicIsLoggedNotFatal checks that a panic in a download leaves a trace
// in the log, fails the task, and leaves the queue able to carry on.
func TestPanicIsLoggedNotFatal(t *testing.T) {
	lg, buf := testLog()
	a := testManager(t, t.TempDir(), "yt-dlp")
	a.log = lg
	a.mgr.logger = lg.logger()
	// A task as startNext leaves it: running, with its command already
	// chosen. No download is started, so nothing logs into the buffer from
	// another goroutine while the test reads it.
	tk := &task{url: "https://x/v", engineID: "yt-dlp", state: statusRunning}
	a.mgr.tasks = append(a.mgr.tasks, tk)
	a.mgr.running = 1

	// The recovery run installs, exercised the way a panicking run would.
	func() {
		defer func() {
			if r := recover(); r != nil {
				a.mgr.log().Error("panic in a download", "url", tk.url,
					"panic", fmt.Sprint(r), "stack", "vdlp.(*manager).run")
				a.mgr.abandon(tk, errTestPanic)
			}
		}()
		panic("test panic")
	}()

	if !strings.Contains(buf.String(), "panic in a download") {
		t.Errorf("the panic was not logged: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "test panic") {
		t.Errorf("the panic's value was not logged: %q", buf.String())
	}
	if tk.state != statusFailed {
		t.Errorf("state after a panic is %v", tk.state)
	}
	if !strings.Contains(tk.err, "test panic") {
		t.Errorf("the task's error is %q", tk.err)
	}
	if a.mgr.active() != 0 {
		t.Errorf("%d downloads still count as running after a panic", a.mgr.active())
	}
	// abandon is safe to call twice: finish has not run, but a second panic
	// must not take the count below zero.
	a.mgr.abandon(tk, errTestPanic)
	if a.mgr.active() != 0 {
		t.Errorf("%d running after a second abandon", a.mgr.active())
	}
}

// errTestPanic is the error a panicking download reports.
var errTestPanic = errString("the app failed while running this download: test panic")

// errString is an error from a string, for a test's expectations.
type errString string

func (e errString) Error() string { return string(e) }

// TestLogsPageShowsEntries checks that the Logs page builds the entries, and
// that its filter narrows them. The page is the only place a user sees what
// the app did, so it has to show the error and not just the successes.
func TestLogsPageShowsEntries(t *testing.T) {
	a, _ := testApp(t)
	a.page = "logs"
	for i := 0; i < 3; i++ {
		a.log.logger().Info("download finished", "url", "https://example.com/z")
	}
	a.log.logger().Warn("asking about a link failed", "url", "https://example.com/x")
	a.log.logger().Error("download failed", "url", "https://example.com/y", "err", "exit status 1")

	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	// Every level is named on the page, so the user can tell them apart.
	for _, want := range []string{"INFO", "WARN", "ERROR"} {
		if !tt.HasText(want) {
			t.Errorf("the page shows no %s entries, texts %q", want, tt.Texts())
		}
	}
	if !tt.HasText("exit status 1") {
		t.Error("the failure's error is not on the page")
	}
	if !tt.HasText("Open log file") {
		t.Error("the page has no way to the log file")
	}

	// The filter narrows what shows.
	a.logFilter = 1
	tt.Frame()
	if !tt.HasText("ERROR") {
		t.Errorf("the errors filter shows nothing, texts %q", tt.Texts())
	}
	if tt.HasText("download finished") {
		t.Error("the errors filter still shows the successes")
	}
	a.logFilter = 2
	tt.Frame()
	if !tt.HasText("WARN") || tt.HasText("download failed") {
		t.Errorf("the warnings filter shows %q", tt.Texts())
	}
}
