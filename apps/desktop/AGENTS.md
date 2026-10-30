# apps/desktop

The desktop app for macOS, Windows and Linux, drawn with MyGo's native UI
(`github.com/egoist/mygo/ui`). It runs in one of two modes, both over the
public HTTP and SSE API: a client of a remote amdl, or a standalone backend
that runs `internal/server#Run` on a loopback listener and talks to a remote
wrapper.

## Rules

- The app reads the backend only through `apps/desktop/internal/client`, in
  both modes, so it never depends on more than the API contract. The one core
  call outside it is `internal/server#Run` in `apps/desktop/internal/mode`.
- `internal/` never imports this directory. Desktop-only code stays here.
- No `AMDL_*` environment variables: `internal/config#UnreadEnv` warns about
  each one. Development switches are command-line flags (`main.go`).
- Store fields change only on the UI thread. Background work hands results
  over through the store's post function (`Window.Update`).
- A remote backend's paths are its machine's: never pass them through
  `path/filepath` or show them as this computer's.
- Colors come from `apps/desktop/internal/ui#Tokens`; an album's palette is
  used only after a contrast check (`ui#Readable`, `ui#AccentFor`).
- No cgo: every target cross-compiles with `CGO_ENABLED=0`.
- MyGo paints absolute children above in-flow ones, in build order: a
  decorative layer goes before an absolute content layer, as the main
  window's view (`apps/desktop/internal/ui#App`) builds it, never into a
  card beside its content.
- ffmpeg and other console programs start through
  `internal/sysproc#NoConsole`, or each start flashes a console on Windows.

## Verify

```bash
go test ./apps/desktop/...
GOOS=windows go vet ./apps/desktop/... && GOOS=darwin go vet ./apps/desktop/... && GOOS=linux go vet ./apps/desktop/...
```

Screens: build the app and run it with `-shots <dir>` (and `-remote` or
`-wrapper`, `-data <dir>`), which captures every screen in a 1280×800 window
with `Window.CapturePage`, renders playing motion covers without a window
(`*-motion-N.png`), and quits. The window shows stills where the desktop asks
for less motion; the renders do not.
