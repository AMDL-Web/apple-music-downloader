module amdl

// Keep this in step with the golang image in deploy/server/Dockerfile. CI resolves its Go
// version from this line (go-version-file: go.mod), so letting the two drift
// means the released binary is built by a toolchain CI never exercised.
go 1.27.3

require (
	github.com/Eyevinn/mp4ff v0.55.0
	github.com/egoist/mygo v0.7.3
	github.com/iyear/gowidevine v0.1.3
	github.com/zhaarey/go-mp4tag v0.0.0-20260509131819-a89fa417cd97
	golang.org/x/time v0.15.0
	google.golang.org/protobuf v1.36.11
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.56.0
)

require (
	github.com/chmike/cmac-go v1.1.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.74.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
