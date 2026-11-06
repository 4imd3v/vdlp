package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// logKeep is how many entries the Logs page keeps in memory. The file keeps
// them all, up to logMaxBytes.
const logKeep = 500

// logMaxBytes is how large the log file may grow before it is rotated, so a
// long-running app cannot fill the disk with its own debugging.
const logMaxBytes = 8 << 20

// entry is one line of the log as the Logs page shows it.
type entry struct {
	at    time.Time
	level slog.Level
	msg   string
	attrs []string
}

// String writes an entry as one line, as the file and the clipboard do.
func (e entry) String() string {
	s := e.at.Format("15:04:05.000") + " " + e.level.String() + " " + e.msg
	for _, a := range e.attrs {
		s += "  " + a
	}
	return s
}

// logHandler is a slog handler that writes every record twice: as a line of
// JSON to the log file, which a tool can read, and to the tail the Logs page
// shows.
type logHandler struct {
	mu      sync.Mutex
	out     io.Writer
	entries []entry
}

func (h *logHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *logHandler) Handle(_ context.Context, r slog.Record) error {
	e := entry{at: r.Time, level: r.Level, msg: r.Message}
	obj := map[string]any{"time": r.Time.Format(time.RFC3339Nano), "level": r.Level.String(), "msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		v := a.Value.Any()
		// An error marshals to "{}" as JSON, which loses the only thing
		// about it anyone reads: what it says.
		if err, ok := v.(error); ok {
			v = err.Error()
		}
		e.attrs = append(e.attrs, a.Key+"="+fmt.Sprint(v))
		obj[a.Key] = v
		return true
	})
	line, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, e)
	if len(h.entries) > logKeep {
		h.entries = h.entries[len(h.entries)-logKeep:]
	}
	_, err = h.out.Write(append(line, '\n'))
	return err
}

// WithAttrs and WithGroup are not used: every record carries its own details.
func (h *logHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logHandler) WithGroup(string) slog.Handler      { return h }

// tail returns the last entries, oldest first.
func (h *logHandler) tail() []entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]entry, len(h.entries))
	copy(out, h.entries)
	return out
}

// appLog is the app's log: where it is written, and the entries it holds.
type appLog struct {
	handler *logHandler
	path    string
}

// openLog starts the log, writing to a file in the app's logs directory. A
// log that has grown past logMaxBytes is rotated to <name>.1, so the history
// of the run before it is kept.
func openLog() (*appLog, error) {
	dir, err := logsDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "vdlp.log")
	if st, err := os.Stat(path); err == nil && st.Size() > logMaxBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &appLog{handler: &logHandler{out: f}, path: path}, nil
}

// logsDir is the app's logs directory.
func logsDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathLogs)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// tail returns the entries the Logs page shows.
func (l *appLog) tail() []entry {
	if l == nil {
		return nil
	}
	return l.handler.tail()
}

// logger returns the slog logger the app logs through. A nil appLog still
// logs, to nowhere: the app must not stop because its log could not open.
func (l *appLog) logger() *slog.Logger {
	if l == nil || l.handler == nil {
		return slog.New(slog.DiscardHandler)
	}
	return slog.New(l.handler)
}

// text returns the whole tail as text, for the clipboard.
func (l *appLog) text() string {
	var b []byte
	for _, e := range l.tail() {
		b = append(b, e.String()...)
		b = append(b, '\n')
	}
	return string(b)
}

// recoverLog turns a panic in a goroutine into a log entry, so a crash leaves
// a trace in the log instead of only a stack on the terminal.
func recoverLog(log *slog.Logger, what string) {
	if r := recover(); r != nil {
		log.Error("panic", "where", what, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
	}
}
