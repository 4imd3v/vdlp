package main

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// icon builds an icon from the shapes of a 24x24 viewBox, stroked as
// Lucide's are, so it takes the color of the text around it.
func icon(shapes ...string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + strings.Join(shapes, "") + `</svg>`))
}

var (
	iconDownload = icon(`<path d="M12 3v12"/><path d="m7 12 5 5 5-5"/><path d="M5 21h14"/>`)
	iconBatch    = icon(`<path d="M8 6h12M8 12h12M8 18h12"/><path d="M4 6h.01M4 12h.01M4 18h.01"/>`)
	iconPlaylist = icon(`<path d="M4 6h10M4 12h10M4 18h6"/><circle cx="18" cy="15" r="4"/><path d="m16.6 13.4 4 1.6-4 1.6z"/>`)
	iconLibrary  = icon(`<path d="M4 20h16a1 1 0 0 0 1-1V9a1 1 0 0 0-1-1h-7.4a2 2 0 0 1-1.5-.7l-1.1-1.2A2 2 0 0 0 8.6 5H5a1 1 0 0 0-1 1z"/>`)
	iconSettings = icon(`<circle cx="12" cy="12" r="3"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>`)
	iconAbout    = icon(`<circle cx="12" cy="12" r="9"/><path d="M12 16v-4M12 8h.01"/>`)

	iconPaste     = icon(`<rect x="8" y="3" width="8" height="4" rx="1"/><path d="M16 5h2a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1h2"/>`)
	iconPlus      = icon(`<path d="M12 5v14M5 12h14"/>`)
	iconClose     = icon(`<path d="M18 6 6 18M6 6l12 12"/>`)
	iconCheck     = icon(`<path d="M20 6 9 17l-5-5"/>`)
	iconPause     = icon(`<rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>`)
	iconResume    = icon(`<path d="M6 4l14 8-14 8z"/>`)
	iconTrash     = icon(`<path d="M4 7h16M10 11v6M14 11v6"/><path d="M6 7l1 13a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-13"/><path d="M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>`)
	iconReveal    = icon(`<path d="M4 20h16a1 1 0 0 0 1-1V9a1 1 0 0 0-1-1h-7.4a2 2 0 0 1-1.5-.7l-1.1-1.2A2 2 0 0 0 8.6 5H5a1 1 0 0 0-1 1z"/><path d="M4 20V9"/>`)
	iconOpen      = icon(`<path d="M14 4h6v6"/><path d="M20 4 10 14"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>`)
	iconRefresh   = icon(`<path d="M21 12a9 9 0 1 1-3-6.7"/><path d="M21 3v6h-6"/>`)
	iconFile      = icon(`<path d="M14 3v5h5"/><path d="M19 21H5a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h9l5 5v12a1 1 0 0 1-1 1z"/>`)
	iconSearch    = icon(`<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`)
	iconWarn      = icon(`<path d="M12 3 2.5 20h19z"/><path d="M12 10v4M12 17h.01"/>`)
	iconSubtitles = icon(`<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 15h4M15 15h2M7 11h2M13 11h4"/>`)
	iconLogs      = icon(`<path d="M5 4h11l3 3v13a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1z"/><path d="M8 10h7M8 14h7M8 18h4"/>`)
	iconExternal  = icon(`<path d="M14 4h6v6"/><path d="M20 4 10 14"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>`)
)
