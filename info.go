package main

import (
	"encoding/json"
	"net/url"
)

// infoJSON is what yt-dlp prints for -J: what a link holds. The fields the
// app does not read are left out rather than named.
type infoJSON struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Duration float64      `json:"duration"`
	Formats  []formatJSON `json:"formats"`
	Entries  []entryJSON  `json:"entries"`
}

type formatJSON struct {
	FormatID       string `json:"format_id"`
	Ext            string `json:"ext"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	Filesize       int64  `json:"filesize"`
	FilesizeApprox int64  `json:"filesize_approx"`
	FormatNote     string `json:"format_note"`
	VCodec         string `json:"vcodec"`
	ACodec         string `json:"acodec"`
}

type entryJSON struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	Webpage  string  `json:"webpage_url"`
	Duration float64 `json:"duration"`
}

// parseInfo reads what an engine printed for -J. flat asks for the entries
// of a playlist, otherwise for the formats of one video; from is the link
// that was asked about, which a playlist's relative entry links resolve
// against.
func parseInfo(data []byte, flat bool, from string) (*mediaInfo, error) {
	var raw infoJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	info := &mediaInfo{title: raw.Title, duration: raw.Duration}
	if info.title == "" {
		info.title = raw.ID
	}
	if flat {
		for _, e := range raw.Entries {
			info.entries = append(info.entries, mediaEntry{
				id:       e.ID,
				title:    e.Title,
				url:      entryURL(from, firstNonEmpty(e.URL, e.Webpage)),
				duration: e.Duration,
			})
		}
		return info, nil
	}
	for _, f := range raw.Formats {
		size := f.Filesize
		if size == 0 {
			size = f.FilesizeApprox
		}
		// A storyboard is a grid of thumbnails, not a stream: it has
		// neither codec, and picking one downloads nothing playable.
		if !hasCodec(f.VCodec) && !hasCodec(f.ACodec) {
			continue
		}
		info.formats = append(info.formats, mediaFormat{
			id:     f.FormatID,
			ext:    f.Ext,
			note:   f.FormatNote,
			width:  f.Width,
			height: f.Height,
			size:   size,
			audio:  hasCodec(f.ACodec),
			video:  hasCodec(f.VCodec),
		})
	}
	return info, nil
}

// entryURL makes an entry's link absolute. A flat playlist gives each entry
// a link relative to the site, "/watch?v=…" say, which a downloader cannot
// fetch on its own; the playlist's own link says what it is relative to.
func entryURL(from, entry string) string {
	if entry == "" {
		return ""
	}
	u, err := url.Parse(entry)
	if err != nil || u.IsAbs() {
		return entry
	}
	base, err := url.Parse(from)
	if err != nil || base.Host == "" {
		return entry
	}
	base.Path, base.RawQuery, base.Fragment = "/", "", ""
	return base.ResolveReference(u).String()
}

// hasCodec reports whether a codec name names a stream rather than none.
func hasCodec(name string) bool {
	return name != "" && name != "none"
}

// firstNonEmpty returns the first string that is not empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
