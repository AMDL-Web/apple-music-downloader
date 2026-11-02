package app

import (
	"time"

	"amdl/apps/desktop/internal/store"
	appui "amdl/apps/desktop/internal/ui"
)

// libraryShots captures the Library: the album records, the playlist
// records, and the Apple Music library's albums, songs and playlists.
func (a *App) libraryShots(shot func(name string, setup func(), settle time.Duration)) {
	tab := func(t store.LibraryTab) func() {
		return func() {
			a.view.Go(appui.ScreenLibrary)
			a.view.LibraryTab(t)
		}
	}
	shot("40-library", tab(store.LibraryAlbums), 3*time.Second)
	shot("41-library-playlists", tab(store.LibraryPlaylists), 2*time.Second)
	shot("42-library-apple", tab(store.LibraryApple), 3*time.Second)
	for i, kind := range store.AppleKinds[1:] {
		shot([]string{"43-library-apple-songs", "44-library-apple-playlists"}[i], func() { a.view.AppleKind(kind) }, 2*time.Second)
	}
	shot("45-library-light", func() { a.view.Theme = "light"; a.view.LibraryTab(store.LibraryAlbums) }, 2*time.Second)
	a.view.Theme = a.opts.Theme
}
