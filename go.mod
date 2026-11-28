module amdl

// Keep this in step with the golang image in deploy/server/Dockerfile. CI resolves its Go
// version from this line (go-version-file: go.mod), so letting the two drift
// means the released binary is built by a toolchain CI never exercised.
go 1.27.4

require (
	charm.land/bubbles/v2 v2.2.2
	charm.land/bubbletea/v2 v2.1.3
	charm.land/lipgloss/v2 v2.1.1
	github.com/Eyevinn/mp4ff v0.64.0
	github.com/charmbracelet/colorprofile v0.4.3
	github.com/charmbracelet/x/ansi v0.11.10
	github.com/charmbracelet/x/term v0.2.2
	github.com/egoist/mygo v0.10.0
	github.com/iyear/gowidevine v0.1.3
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/rogpeppe/go-internal v1.16.0
	github.com/zhaarey/go-mp4tag v0.0.0-20260509131819-a89fa417cd97
	golang.org/x/image v0.48.0
	golang.org/x/sys v0.50.0
	golang.org/x/time v0.16.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.63.0
)

require (
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20261008173134-6b8d4baf91b4 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/chmike/cmac-go v1.1.0 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/ebitengine/purego v0.11.3 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.1 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-runewidth v0.0.31 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.8-0.20261110175422-3a370411ca06 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	github.com/xo/terminfo v1.0.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

tool github.com/egoist/mygo/cmd/mygo
