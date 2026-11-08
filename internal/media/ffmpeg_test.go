package media

import "testing"

func TestFFmpegInstallHint(t *testing.T) {
	only := func(names ...string) func(string) bool {
		return func(cmd string) bool {
			for _, n := range names {
				if n == cmd {
					return true
				}
			}
			return false
		}
	}
	for _, c := range []struct {
		goos string
		has  []string
		want string
	}{
		{"darwin", []string{"brew", "port"}, "brew install ffmpeg"},
		{"darwin", nil, "install Homebrew from https://brew.sh, then brew install ffmpeg"},
		{"linux", []string{"apt-get"}, "sudo apt-get install ffmpeg"},
		{"linux", []string{"dnf"}, "sudo dnf install ffmpeg-free"},
		{"linux", []string{"pacman"}, "sudo pacman -S ffmpeg"},
		{"windows", []string{"scoop", "choco"}, "scoop install ffmpeg"},
		{"windows", []string{"winget", "scoop"}, "winget install --id Gyan.FFmpeg -e"},
		{"freebsd", nil, "download ffmpeg from https://ffmpeg.org/download.html and put it on PATH"},
	} {
		if got := FFmpegInstallHint(c.goos, only(c.has...)); got != c.want {
			t.Errorf("%s with %v: %q, want %q", c.goos, c.has, got, c.want)
		}
	}
}

func TestParseFFmpegVersion(t *testing.T) {
	// The first lines of ffmpeg -version from a Homebrew and a Debian build.
	for out, want := range map[string]string{
		"ffmpeg version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers\nbuilt with Apple clang\n": "7.1.1",
		"ffmpeg version 5.1.6-0+deb12u1 Copyright (c) 2000-2024 the FFmpeg developers\n":               "5.1.6-0+deb12u1",
		"not ffmpeg": "",
	} {
		if got := parseFFmpegVersion(out); got != want {
			t.Errorf("parseFFmpegVersion(%q) = %q, want %q", out, got, want)
		}
	}
}

func TestFFmpegOutdated(t *testing.T) {
	for v, want := range map[string]bool{"7.0.2": true, "6.1.1-3ubuntu5": true, "8.0.1": false, "8.1.3": false, "10.0": false, "N-118312-g1d5b2a3": false, "": false} {
		if got := FFmpegOutdated(v); got != want {
			t.Errorf("FFmpegOutdated(%q) = %v, want %v", v, got, want)
		}
	}
}
