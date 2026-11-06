package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
)

// engine is one of the command line downloaders the app drives, as Media
// Downloader's extensions are. Each knows the arguments its presets need.
type engine struct {
	id      string
	name    string
	bin     string
	summary string
	// extra are arguments every run of this engine gets.
	extra []string
	// outFlag is the flag that takes a filename template, "" when the
	// engine has none the app can offer.
	outFlag string
	// cookiesFlag is the flag that takes a cookies file.
	cookiesFlag string
	// lineFlag makes the engine print each progress line on its own.
	lineFlag string
	// resumeFlag continues a partial file, "" when the engine cannot.
	resumeFlag string
	// subsArgs download the subtitles beside the video, nil when the
	// engine has no way to. The app muxes them in itself (see
	// embedSubtitles), so it can rewrite them first: YouTube's vtt of its
	// automatic captions repeats the line before each one, so the engine
	// is asked for its native srt, one line per cue.
	subsArgs []string
	// formats reports whether the engine can list formats and playlist
	// entries, as yt-dlp does with -J.
	formats bool
	presets []preset
}

// preset is one choice in the download options list: a name and the
// arguments it adds to the command line.
type preset struct {
	name string
	args []string
}

// subtitleArgs ask for the subtitles of a video and nothing else: its own
// where it has them, its automatic captions otherwise, as the native srt
// that is one line per cue, which cleanSRT rewrites into blocks.
var subtitleArgs = []string{"--skip-download", "--write-subs", "--write-auto-subs",
	"--sub-format", "srt/vtt/best"}

func ytdlpPresets() []preset {
	video := func(h int) preset {
		return preset{fmt.Sprintf("%dp", h), []string{"-f", fmt.Sprintf("bv*[height<=%d]+ba/b[height<=%d]", h, h)}}
	}
	audio := func(f string) preset {
		return preset{"Audio: " + strings.ToUpper(f), []string{"-x", "--audio-format", f, "--audio-quality", "0"}}
	}
	return []preset{
		{"Best (video + audio)", []string{"-f", "bv*+ba/b"}},
		{"Best video only", []string{"-f", "bv*"}},
		video(2160), video(1440), video(1080), video(720), video(480), video(360),
		{"Audio only (best)", []string{"-f", "ba/bestaudio"}},
		audio("mp3"), audio("m4a"), audio("opus"), audio("vorbis"), audio("flac"),
		{"Subtitles only", append(slices.Clone(subtitleArgs), "--sub-langs", "all")},
		{"Thumbnail only", []string{"--skip-download", "--write-thumbnail", "--convert-thumbnails", "png"}},
		{"Metadata only", []string{"--skip-download", "--write-info-json"}},
	}
}

// engines are the downloaders the app can drive, in the order the sidebar
// and the settings list them.
var engines = []engine{
	{
		id: "yt-dlp", name: "yt-dlp", bin: "yt-dlp",
		summary:     "YouTube and 1000+ other sites. The default.",
		outFlag:     "-o",
		cookiesFlag: "--cookies",
		lineFlag:    "--newline",
		resumeFlag:  "-c",
		subsArgs: []string{"--write-subs", "--write-auto-subs",
			"--sub-format", "srt/vtt/best"},
		formats: true,
		presets: ytdlpPresets(),
	},
	{
		id: "youtube-dl", name: "youtube-dl", bin: "youtube-dl",
		summary:     "The older fork, for the sites yt-dlp dropped.",
		outFlag:     "-o",
		cookiesFlag: "--cookies",
		lineFlag:    "--newline",
		resumeFlag:  "-c",
		subsArgs: []string{"--write-sub", "--write-auto-sub",
			"--sub-format", "srt/vtt/best"},
		formats: true,
		presets: ytdlpPresets(),
	},
	{
		id: "gallery-dl", name: "gallery-dl", bin: "gallery-dl",
		summary:     "Image galleries: Imgur, Flickr, DeviantArt, Reddit, Twitter.",
		cookiesFlag: "--cookies",
		presets: []preset{
			{"Best", nil},
			{"Metadata only (JSON)", []string{"--dump-json"}},
		},
	},
	{
		id: "lux", name: "lux", bin: "lux",
		summary:     "Fast video downloader for Chinese sites: Bilibili, Weibo.",
		outFlag:     "-o",
		cookiesFlag: "--cookies",
		formats:     true,
		presets: []preset{
			{"Best", nil},
			{"Audio only", []string{"--audio-only"}},
		},
	},
	{
		id: "you-get", name: "you-get", bin: "you-get",
		summary: "Youku, iQiyi, Mango TV and other Chinese sites.",
		outFlag: "-o",
		presets: []preset{
			{"Best", nil},
			{"Info only", []string{"-i"}},
		},
	},
	{
		id: "svtplay-dl", name: "svtplay-dl", bin: "svtplay-dl",
		summary:     "SVT Play and other Swedish streaming services.",
		cookiesFlag: "--cookies",
		presets: []preset{
			{"Best", nil},
			{"Audio only", []string{"-A"}},
			{"Subtitles only", []string{"-S"}},
		},
	},
	{
		id: "aria2c", name: "aria2c", bin: "aria2c",
		summary:     "Multi-connection downloader for direct links and torrents.",
		cookiesFlag: "--load-cookies",
		lineFlag:    "--summary-interval=1",
		resumeFlag:  "-c",
		presets: []preset{
			{"Download", nil},
			{"16 connections", []string{"-x", "16", "-s", "16"}},
			{"Continue partial files", []string{"-c"}},
			{"Torrents and metalinks", []string{"--follow-torrent=true", "--follow-metalink=true"}},
		},
	},
	{
		id: "wget", name: "wget", bin: "wget",
		summary:     "Retrieves files and mirrors websites over HTTP.",
		cookiesFlag: "--load-cookies",
		resumeFlag:  "-c",
		presets: []preset{
			{"Download", nil},
			{"Continue partial files", []string{"-c"}},
			{"Mirror website", []string{"--mirror", "--convert-links", "--adjust-extension", "--page-requisites", "--no-parent"}},
		},
	},
	{
		id: "safari-books", name: "Safari Books", bin: "yt-dlp",
		summary:     "Safari Books Online, through yt-dlp with the browser's cookies.",
		extra:       safariCookies(),
		outFlag:     "-o",
		cookiesFlag: "--cookies",
		lineFlag:    "--newline",
		resumeFlag:  "-c",
		formats:     true,
		presets: []preset{
			{"Best (video + audio)", []string{"-f", "bv*+ba/b"}},
			{"Audio only (MP3)", []string{"-x", "--audio-format", "mp3", "--audio-quality", "0"}},
			{"Book pages (images)", []string{"-f", "bv*"}},
		},
	},
}

// safariCookies reads Safari's cookies where only Safari keeps them.
func safariCookies() []string {
	if runtime.GOOS == "darwin" {
		return []string{"--cookies-from-browser", "safari"}
	}
	return nil
}

// engineByID returns the engine with that id, or the default one.
func engineByID(id string) engine {
	for _, e := range engines {
		if e.id == id {
			return e
		}
	}
	return engines[0]
}

// engineNames returns the engines' labels, as the select shows them.
func engineNames() []string {
	names := make([]string, len(engines))
	for i, e := range engines {
		names[i] = e.name
	}
	return names
}

// idForName returns the id of the engine with that label, "" for none.
func idForName(name string) string {
	for _, e := range engines {
		if e.name == name {
			return e.id
		}
	}
	return ""
}

// engineIndex returns the engine's place in engines, -1 for none.
func engineIndex(id string) int {
	for i, e := range engines {
		if e.id == id {
			return i
		}
	}
	return -1
}

// binary returns the executable to run: the path the user set, else the
// engine's own name looked up in PATH.
func (e engine) binary(override string) string {
	if override != "" {
		return override
	}
	return e.bin
}

// installed reports whether the engine's binary can be found.
func (e engine) installed(override string) bool {
	_, err := exec.LookPath(e.binary(override))
	return err == nil
}

// mediaExts are the extensions of files a browser downloads rather than a
// video site's extractor.
var mediaExts = map[string]bool{
	"mp4": true, "mkv": true, "webm": true, "mov": true, "avi": true, "flv": true,
	"mp3": true, "m4a": true, "opus": true, "ogg": true, "flac": true, "wav": true,
	"zip": true, "tar": true, "gz": true, "7z": true, "rar": true, "iso": true,
	"pdf": true, "epub": true, "apk": true, "exe": true, "msi": true, "dmg": true,
	"deb": true, "rpm": true, "png": true, "jpg": true, "jpeg": true, "gif": true,
	"webp": true, "torrent": true,
}

// routes name the hosts whose links belong to one engine rather than the
// default, as Media Downloader guesses from the link's host.
var routes = []struct{ domain, id string }{
	{"svtplay.se", "svtplay-dl"},
	{"svt.se", "svtplay-dl"},
	{"oppetarkival", "svtplay-dl"},
	{"bilibili.com", "lux"},
	{"b23.tv", "lux"},
	{"youku.com", "you-get"},
	{"iqiyi.com", "you-get"},
	{"v.qq.com", "you-get"},
	{"mgtv.com", "you-get"},
	{"acfun.cn", "you-get"},
	{"imgur.com", "gallery-dl"},
	{"deviantart.com", "gallery-dl"},
	{"flickr.com", "gallery-dl"},
	{"tumblr.com", "gallery-dl"},
	{"pixiv.net", "gallery-dl"},
	{"artstation.com", "gallery-dl"},
	{"newgrounds.com", "gallery-dl"},
	{"danbooru.donmai.us", "gallery-dl"},
	{"gelbooru.com", "gallery-dl"},
	{"weibo.com", "gallery-dl"},
	{"twitter.com", "gallery-dl"},
	{"x.com", "gallery-dl"},
	{"reddit.com", "gallery-dl"},
	{"instagram.com", "gallery-dl"},
}

// engineFor guesses which engine can fetch url, from its host or the file
// it names.
func engineFor(url string) string {
	u := strings.TrimSpace(url)
	switch {
	case strings.HasPrefix(u, "magnet:"), strings.HasSuffix(u, ".torrent"):
		return "aria2c"
	}
	if ext := strings.ToLower(extOf(u)); mediaExts[ext] {
		return "aria2c"
	}
	host := hostOf(u)
	if host == "" {
		return "yt-dlp"
	}
	for _, r := range routes {
		if host == r.domain || strings.HasSuffix(host, "."+r.domain) {
			return r.id
		}
	}
	return "yt-dlp"
}

// hostOf returns the lower-case host of an http(s) URL, without "www.".
func hostOf(raw string) string {
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ToLower(s)
	return strings.TrimPrefix(s, "www.")
}

// extOf returns a URL's extension without the dot, "" for none.
func extOf(u string) string {
	s := u
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	i := strings.LastIndexByte(s, '.')
	if i < 0 || i == len(s)-1 {
		return ""
	}
	return s[i+1:]
}
