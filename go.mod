module amdl

// Keep this in step with the golang image in deploy/server/Dockerfile. CI resolves its Go
// version from this line (go-version-file: go.mod), so letting the two drift
// means the released binary is built by a toolchain CI never exercised.
go 1.27.3

require (
	github.com/Eyevinn/mp4ff v0.61.0
	github.com/iyear/gowidevine v0.1.3
	github.com/zhaarey/go-mp4tag v0.0.0-20260509131819-a89fa417cd97
	golang.org/x/time v0.16.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.61.0
)

require (
	github.com/chmike/cmac-go v1.1.0 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.49.0 // indirect
	modernc.org/libc v1.79.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
