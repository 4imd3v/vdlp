package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// settings is what the user configures, kept as JSON in the app's data
// directory. The view binds its controls straight to these fields, so the
// app keeps a stable pointer to the struct the window reads.
type settings struct {
	// Engine is the engine new downloads use; "" for the default.
	Engine string `json:"engine"`
	// Presets remembers the last preset chosen per engine id.
	Presets map[string]string `json:"presets,omitempty"`
	// Binaries overrides where each engine's executable is found.
	Binaries map[string]string `json:"binaries,omitempty"`
	// DownloadDir is where downloads are written.
	DownloadDir string `json:"downloadDir"`
	// Template is the filename template given to engines that take one.
	Template string `json:"template"`
	// MaxActive is how many downloads run at once.
	MaxActive int `json:"maxActive"`
	// Cookies is a cookies file passed to engines that take one.
	Cookies string `json:"cookies"`
	// Proxy is a proxy URL given to the downloaders' environment.
	Proxy string `json:"proxy"`
	// KeepAwake keeps the computer awake while downloads run.
	KeepAwake bool `json:"keepAwake"`
	// Notify sends a notification when a download ends.
	Notify bool `json:"notify"`
}

func defaultSettings() settings {
	return settings{
		Engine:    engines[0].id,
		Presets:   map[string]string{},
		Binaries:  map[string]string{},
		Template:  "%(title)s [%(id)s].%(ext)s",
		MaxActive: 3,
		KeepAwake: true,
		Notify:    true,
	}
}

// settingsPath is the JSON file the settings are kept in.
func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// loadSettings reads the settings, filling in defaults for what the file
// leaves out.
func loadSettings() (settings, error) {
	s := defaultSettings()
	path, err := settingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return defaultSettings(), err
	}
	if s.MaxActive < 1 {
		s.MaxActive = 1
	}
	if s.MaxActive > 16 {
		s.MaxActive = 16
	}
	if s.Presets == nil {
		s.Presets = map[string]string{}
	}
	if s.Binaries == nil {
		s.Binaries = map[string]string{}
	}
	if s.DownloadDir == "" {
		if dir, err := mygo.App.Path(mygo.PathDownloads); err == nil {
			s.DownloadDir = dir
		}
	}
	if engineIndex(s.Engine) < 0 {
		s.Engine = engines[0].id
	}
	return s, nil
}

// save writes the settings to disk.
func (s *settings) save() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
