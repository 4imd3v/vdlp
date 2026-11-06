package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestRenderPages draws every page to a PNG, to look at the layout without a
// window: ui.Render runs the view once at a given size and scale. The files
// go to the directory the test is given, or to /tmp when none is asked for.
func TestRenderPages(t *testing.T) {
	set := defaultSettings()
	set.Notify, set.KeepAwake = false, false
	a := newApp(set)
	a.mgr.max = 0

	// A library with something of every state in it.
	a.queue("https://example.com/never-gonna-give-you-up.mp4", "Best", []string{"-f", "bv*+ba/b"})
	a.mgr.setState(a.mgr.snapshot()[0].id, statusDone)
	a.queue("https://example.com/another.mkv", "Best", nil)
	a.mgr.setState(a.mgr.snapshot()[1].id, statusRunning)
	a.queue("https://example.com/broken.zip", "Best", nil)
	a.mgr.setState(a.mgr.snapshot()[2].id, statusFailed)
	a.batch = []string{"https://example.com/a.mp4", "https://example.com/b.mp4"}
	a.info = &mediaInfo{
		title:    "A playlist of things",
		duration: 3661,
		entries: []mediaEntry{
			{"1", "First entry", "https://example.com/1", 95},
			{"2", "Second entry", "https://example.com/2", 61},
			{"3", "Third entry", "https://example.com/3", 0},
		},
		formats: []mediaFormat{
			{id: "137", ext: "mp4", note: "1080p", width: 1920, height: 1080, size: 12345678, video: true},
			{id: "248", ext: "webm", note: "1080p", width: 1920, height: 1080, size: 9000000, video: true},
			{id: "251", ext: "opus", note: "", size: 3000000, audio: true},
			{id: "18", ext: "mp4", note: "360p", width: 640, height: 360, size: 1000000, video: true, audio: true},
			{id: "22", ext: "mp4", note: "720p", width: 1280, height: 720, size: 5000000, video: true, audio: true},
			{id: "best", ext: "mp4", note: "best", width: 1920, height: 1080, size: 20000000, video: true, audio: true},
		},
	}
	dir := os.Getenv("VDLP_RENDER_DIR")
	if dir == "" {
		dir = "/tmp"
	}
	for _, p := range pages {
		a.page = p.id
		a.infoFor = a.url
		a.infoFlat = p.id == "playlist"
		a.chosen = map[string]bool{"137": true, "2": true}
		img := ui.Render(a.view, 1100, 760, 1)
		path := filepath.Join(dir, fmt.Sprintf("vdlp-%s.png", p.id))
		if err := writePNG(path, img); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
	}
}

// writePNG encodes an image as a PNG file.
func writePNG(path string, img *image.RGBA) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
