package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/egoist/mygo"
)

// TestSettingsRoundTrip checks that what the user sets survives a restart,
// and that a broken file falls back to the defaults rather than failing.
func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)

	s := defaultSettings()
	s.DownloadDir = filepath.Join(dir, "incoming")
	s.MaxActive = 7
	s.Cookies = "/tmp/cookies.txt"
	s.Proxy = "http://127.0.0.1:8080"
	s.Presets["yt-dlp"] = "1080p"
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Errorf("no settings file: %v", err)
	}
	got, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.DownloadDir != s.DownloadDir || got.MaxActive != 7 || got.Cookies != "/tmp/cookies.txt" ||
		got.Proxy != "http://127.0.0.1:8080" || got.Presets["yt-dlp"] != "1080p" {
		t.Errorf("settings did not round-trip: %+v", got)
	}

	os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{not json"), 0o644)
	got, err = loadSettings()
	if err == nil {
		t.Error("a broken settings file gave no error")
	}
	if got.MaxActive != defaultSettings().MaxActive {
		t.Errorf("a broken settings file gave %+v", got)
	}
}

// TestSettingsClampConcurrency checks the limits the queue is given.
func TestSettingsClampConcurrency(t *testing.T) {
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	for _, n := range []int{-3, 0, 1, 99} {
		path := filepath.Join(dir, "settings.json")
		os.WriteFile(path, []byte(`{"maxActive":`+strconv.Itoa(n)+`}`), 0o644)
		got, err := loadSettings()
		if err != nil {
			t.Fatal(err)
		}
		if got.MaxActive < 1 || got.MaxActive > 16 {
			t.Errorf("maxActive %d became %d", n, got.MaxActive)
		}
	}
}
