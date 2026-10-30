package ui

import (
	"github.com/egoist/mygo/ui"
)

// icon parses a 16×16 stroke icon of the design.
func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

// The design's icons, drawn on a 16-unit grid with round strokes.
var (
	icQueue    = icon(`<path d="M3 4h10M3 8h10M3 12h6"/>`)
	icLibrary  = icon(`<rect x="2.5" y="2.5" width="4.5" height="4.5" rx="1"/><rect x="9" y="2.5" width="4.5" height="4.5" rx="1"/><rect x="2.5" y="9" width="4.5" height="4.5" rx="1"/><rect x="9" y="9" width="4.5" height="4.5" rx="1"/>`)
	icSystem   = icon(`<path d="M2 8h3l2-4 2 8 2-4h3"/>`)
	icSettings = icon(`<circle cx="8" cy="8" r="2.2"/><path d="M8 1.8v1.6M8 12.6v1.6M1.8 8h1.6M12.6 8h1.6M3.6 3.6l1.1 1.1M11.3 11.3l1.1 1.1M3.6 12.4l1.1-1.1M11.3 4.7l1.1-1.1"/>`)
	icLink     = icon(`<path d="M6.5 9.5 9.5 6.5M7 4.5l1-1a2.8 2.8 0 0 1 4 4l-1 1M9 11.5l-1 1a2.8 2.8 0 0 1-4-4l1-1"/>`)
	icSearch   = icon(`<circle cx="7" cy="7" r="4.5"/><path d="M10.5 10.5 14 14"/>`)
	icBack     = icon(`<path d="M10 3 5 8l5 5"/>`)
	icMore     = icon(`<path d="M4 8h.01M8 8h.01M12 8h.01" stroke-width="2.2"/>`)
	icChevron  = icon(`<path d="M4.5 6.5 8 10l3.5-3.5"/>`)
	icDown     = icon(`<path d="M8 2.7v8M4.7 7.7 8 11l3.3-3.3"/>`)
	icLock     = icon(`<rect x="4" y="7" width="8" height="6" rx="1.2"/><path d="M5.7 7V5.3a2.3 2.3 0 0 1 4.6 0V7"/>`)
	icClock    = icon(`<circle cx="8" cy="8" r="4.7"/><path d="M8 5.6v2.6l1.6 1.1"/>`)
	icCheck    = icon(`<path d="M3.5 8.3 6.6 11.4 12.5 4.8"/>`)
	icCross    = icon(`<path d="M5 5l6 6M11 5l-6 6"/>`)
	icWarn     = icon(`<path d="M8 4v4.7M8 11.2h.01"/>`)
	icServer   = icon(`<rect x="2" y="2.7" width="12" height="4" rx="1"/><rect x="2" y="9.3" width="12" height="4" rx="1"/><path d="M4.7 4.7h.01M4.7 11.3h.01"/>`)
	icLaptop   = icon(`<rect x="2" y="2.7" width="12" height="8" rx="1"/><path d="M5.3 13.3h5.4M8 10.7v2.6"/>`)
	icPlus     = icon(`<path d="M8 3v10M3 8h10"/>`)
	icDisk     = icon(`<rect x="2.5" y="4" width="11" height="8" rx="1.5"/><path d="M5 10h3"/>`)
	icPerson   = icon(`<circle cx="8" cy="6" r="2.6"/><path d="M3 13.5c.8-2.4 2.8-3.5 5-3.5s4.2 1.1 5 3.5"/>`)
	icSync     = icon(`<path d="M3 8a5 5 0 0 1 9-3M13 8a5 5 0 0 1-9 3M12 2v3h-3M4 14v-3h3"/>`)
	icNote     = icon(`<path d="M6 12V4l7-1.5V10"/><circle cx="4.5" cy="12" r="1.6"/><circle cx="11.5" cy="10" r="1.6"/>`)
	icVideo    = icon(`<rect x="2" y="4" width="12" height="8" rx="1.5"/><path d="M7 6.5v3l2.5-1.5z"/>`)
	icPlay     = icon(`<path d="M5 3.5v9l7-4.5z" fill="currentColor" stroke="none"/>`)
)
