package app

import (
	"time"

	"amdl/apps/desktop/internal/store"
	appui "amdl/apps/desktop/internal/ui"
)

// libraryShots captures the Library: the Apple Music library's albums,
// songs and playlists.
func (a *App) libraryShots(shot func(name string, setup func(), settle time.Duration)) {
	shot("42-library-apple", func() { a.view.Go(appui.ScreenLibrary) }, 3*time.Second)
	for i, kind := range store.AppleKinds[1:] {
		shot([]string{"43-library-apple-songs", "44-library-apple-playlists"}[i], func() { a.view.AppleKind(kind) }, 2*time.Second)
	}
	shot("45-library-light", func() { a.view.Theme = "light"; a.view.AppleKind(store.AppleKinds[0]) }, 2*time.Second)
	a.view.Theme = a.opts.Theme
}
