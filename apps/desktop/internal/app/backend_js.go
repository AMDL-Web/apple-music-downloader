//go:build js

package app

// A browser runs no backend of its own: the build for it is the demo
// (Options.Demo), which offers no standalone mode, and asking for one says
// so.

type standaloneBackend struct{ backend *ownBackend }

type ownBackend struct{}

func (a *App) startLocal(int, *ownBackend) {
	a.startErr = "this computer's backend does not run in a browser"
}

func (a *App) stopBackend(*ownBackend) {}

func (a *App) wrapperDir() string { return "" }

// Shared reports no backend of another program: a browser has none.
func (a *App) Shared() string { return "" }
