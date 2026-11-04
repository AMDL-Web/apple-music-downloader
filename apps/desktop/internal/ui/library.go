package ui

import (
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/client"
)

// libraryForm is the Library screen's own state: its Apple list as the
// segmented control holds it.
type libraryForm struct {
	apple int
	// kindAt is when another kind or account was chosen (switchMotion).
	kindAt time.Time
	// appleLists are the places of the Apple lists, by kind, and addedAt
	// when each Apple item's Add was clicked, by URL.
	appleLists map[string]*ui.ListState
	addedAt    map[string]time.Time
}

// libraryScreen is the Apple Music account library to add from. What amdl
// keeps is in the queue.
func (a *App) libraryScreen(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	if !s.Library.Open() {
		s.OpenLibrary()
	}
	if s.Conn.Phase == client.Offline {
		a.offlineBanner(c, k)
	}
	ui.Row(c).Padding(14, 18, 10, 18).Gap(12).AlignItems(ui.Center).Background(k.Ground).Children(func() {
		display(c, "Library", 22).SingleLine()
		ui.Spacer(c)
		a.appleSyncBar(c, k, compact)
	})
	a.appleList(c, k)
}

func joinParts(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}
	return out
}
