package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// cueLines is how many lines of a caption a player draws at once. Every
// block holds that many, so the subtitles keep one height and one place
// instead of moving up as each new line arrives.
const cueLines = 2

// cue is one line of a caption: when it is said, and what it says.
type cue struct {
	start, end int64 // milliseconds
	text       string
}

// parseSRT reads an srt file's cues. Its numbering is not needed: the
// timing line says where a cue begins and ends, the lines after it say.
func parseSRT(s string) []cue {
	var cues []cue
	for _, block := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n\n") {
		lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
		i := 0
		for i < len(lines) && !strings.Contains(lines[i], "-->") {
			i++
		}
		if i == len(lines) {
			continue
		}
		from, to, _ := strings.Cut(lines[i], "-->")
		start, ok1 := parseTime(from)
		end, ok2 := parseTime(to)
		if !ok1 || !ok2 {
			continue
		}
		text := strings.TrimSpace(strings.Join(lines[i+1:], " "))
		cues = append(cues, cue{start, end, text})
	}
	return cues
}

// parseTime reads a cue's "00:00:02,960" into milliseconds, and vtt's
// coordinates after it are ignored.
func parseTime(s string) (int64, bool) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0, false
	}
	parts := strings.Split(strings.ReplaceAll(f[0], ",", "."), ":")
	if len(parts) != 3 {
		return 0, false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	sec, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, false
	}
	return int64(h)*3600000 + int64(m)*60000 + int64(math.Round(sec*1000)), true
}

// formatTime writes milliseconds as an srt timestamp.
func formatTime(ms int64) string {
	const hour, minute, second = 3600000, 60000, 1000
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/hour, ms%hour/minute, ms%minute/second, ms%second)
}

// cleanSRT turns a caption written a line at a time, as YouTube's automatic
// ones are, into blocks of the two lines a player draws. Each block lasts
// from its first line to the start of the next block, so no cue covers a
// line of another and the subtitles never grow or shrink by a line.
func cleanSRT(s string) string {
	var lines []cue
	for _, c := range parseSRT(s) {
		if c.text == "" {
			continue
		}
		// An automatic caption repeats a line as its window rolls.
		if n := len(lines); n > 0 && lines[n-1].text == c.text {
			continue
		}
		lines = append(lines, c)
	}
	var out strings.Builder
	for i, n := 0, 0; i < len(lines); i += cueLines {
		end, text := lines[i].end, lines[i].text
		if i+1 < len(lines) {
			text += "\n" + lines[i+1].text
			end = lines[i+1].end
			if i+cueLines < len(lines) && lines[i+cueLines].start < end {
				end = lines[i+cueLines].start
			}
		}
		if end <= lines[i].start {
			end = lines[i].start + 500
		}
		n++
		fmt.Fprintf(&out, "%d\n%s --> %s\n%s\n\n", n, formatTime(lines[i].start), formatTime(end), text)
	}
	return out.String()
}

// subtitleFiles are what the engine wrote beside its video: the same name
// with a language and an extension added. The folder is read rather than
// matching a pattern: a title names itself "[id]", which a pattern reads
// as the syntax of a character class.
func subtitleFiles(video string) []string {
	base := strings.TrimSuffix(video, filepath.Ext(video))
	dir, prefix := filepath.Split(base)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix+".") {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".srt", ".vtt", ".ass", ".ssa", ".ttml":
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// muxTarget is where the subtitles can go: the video itself in a container
// that holds them, and an mkv beside it in one that does not, as webm
// takes no text track.
func muxTarget(video string) (out, codec string) {
	ext := strings.ToLower(filepath.Ext(video))
	switch ext {
	case ".mp4", ".m4v", ".mov":
		return video, "mov_text"
	case ".mkv", ".mka":
		return video, "srt"
	}
	return strings.TrimSuffix(video, filepath.Ext(video)) + ".mkv", "srt"
}

// embedSubtitles muxes the subtitles into the video, leaving it whole if
// ffmpeg fails: it is written under a temporary name and moved into place.
func embedSubtitles(ctx context.Context, video string, subs []string) (string, error) {
	out, codec := muxTarget(video)
	tmp := strings.TrimSuffix(out, filepath.Ext(out)) + ".vdlp" + filepath.Ext(out)
	args := []string{"-y", "-v", "error", "-i", video}
	for _, s := range subs {
		args = append(args, "-i", s)
	}
	args = append(args, "-map", "0")
	for i := range subs {
		args = append(args, "-map", strconv.Itoa(i+1)+":0")
	}
	args = append(args, "-c", "copy", "-c:s", codec)
	for i, s := range subs {
		if lang := subLang(video, s); lang != "" {
			args = append(args, fmt.Sprintf("-metadata:s:s:%d", i), "language="+lang)
		}
	}
	args = append(args, tmp)
	if b, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("ffmpeg %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(b)))
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if out != video {
		os.Remove(video)
	}
	return out, nil
}

// subLang is the language a subtitle file is named for, as "en" in
// "a title [id].en.srt".
func subLang(video, sub string) string {
	base := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	name := filepath.Base(sub)
	if !strings.HasPrefix(name, base+".") {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(name, base+"."), filepath.Ext(name))
}
