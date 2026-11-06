package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCleanSRT checks that a caption written a line at a time, as YouTube's
// automatic ones are, comes out as blocks of the two lines a player draws:
// no line repeated within a block, and no block covering a line of another,
// which is what made the subtitles move up as each line arrived.
func TestCleanSRT(t *testing.T) {
	const rolling = `1
00:00:00,000 --> 00:00:03,240
Last week, while you were busy

2
00:00:01,480 --> 00:00:05,160
researching how to buy emergency rice

3
00:00:03,240 --> 00:00:06,320
and beans in bulk, a group of guys who

4
00:00:05,160 --> 00:00:08,000
and beans in bulk, a group of guys who

5
00:00:08,000 --> 00:00:09,000
technically aren't supposed to have
`
	got := strings.TrimSpace(cleanSRT(rolling))
	blocks := strings.Split(got, "\n\n")
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks, want 2:\n%s", len(blocks), got)
	}
	// 1-2, then 3-5 with the repeat dropped.
	for i, want := range [][]string{
		{"00:00:00,000 --> 00:00:03,240", "Last week, while you were busy", "researching how to buy emergency rice"},
		{"00:00:03,240 --> 00:00:09,000", "and beans in bulk, a group of guys who", "technically aren't supposed to have"},
	} {
		// The number, the timing, and the two lines a player draws.
		lines := strings.Split(blocks[i], "\n")
		if len(lines) != 4 {
			t.Fatalf("block %d has %d lines:\n%s", i+1, len(lines), blocks[i])
		}
		for j, w := range want {
			if lines[j+1] != w {
				t.Errorf("block %d line %d = %q, want %q", i+1, j+1, lines[j+1], w)
			}
		}
	}
}

// TestParseSRT checks the cues of a file, and that one without a timing line
// is left out rather than breaking the read.
func TestParseSRT(t *testing.T) {
	cues := parseSRT("not a cue\n\n1\n00:00:01,500 --> 00:00:02,250\nhello\nthere\n")
	if len(cues) != 1 {
		t.Fatalf("%d cues, want 1", len(cues))
	}
	if cues[0].start != 1500 || cues[0].end != 2250 || cues[0].text != "hello there" {
		t.Errorf("cue %+v", cues[0])
	}
}

// TestSubtitleFiles checks that the files beside a video are found even when
// its title holds what a pattern would read as syntax, as "[id]" does.
func TestSubtitleFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a title [id].mkv", "a title [id].en.srt", "a title [id].ja.srt", "other.en.srt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := subtitleFiles(filepath.Join(dir, "a title [id].mkv"))
	if len(got) != 2 {
		t.Fatalf("found %v, want the two subtitles of this video", got)
	}
}
