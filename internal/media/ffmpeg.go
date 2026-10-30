package media

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"amdl/internal/sysproc"
)

// FFmpegInstallHint is the command that installs ffmpeg on goos with the
// first package manager has finds, or where to download it.
func FFmpegInstallHint(goos string, has func(cmd string) bool) string {
	type way struct{ manager, command string }
	var ways []way
	switch goos {
	case "darwin":
		ways = []way{{"brew", "brew install ffmpeg"}, {"port", "sudo port install ffmpeg"}}
	case "linux":
		ways = []way{
			{"apt-get", "sudo apt-get install ffmpeg"},
			// Fedora's own repositories carry ffmpeg-free; "ffmpeg" needs RPM Fusion.
			{"dnf", "sudo dnf install ffmpeg-free"},
			{"pacman", "sudo pacman -S ffmpeg"},
			{"apk", "sudo apk add ffmpeg"},
		}
	case "windows":
		ways = []way{{"winget", "winget install --id Gyan.FFmpeg -e"}, {"scoop", "scoop install ffmpeg"}, {"choco", "choco install ffmpeg"}}
	}
	for _, w := range ways {
		if has(w.manager) {
			return w.command
		}
	}
	if goos == "darwin" {
		return "install Homebrew from https://brew.sh, then brew install ffmpeg"
	}
	return "download ffmpeg from https://ffmpeg.org/download.html and put it on PATH"
}

// InstallHint is FFmpegInstallHint for this machine.
func InstallHint() string {
	return FFmpegInstallHint(runtime.GOOS, func(cmd string) bool {
		_, err := exec.LookPath(cmd)
		return err == nil
	})
}

// versionTimeout bounds ffmpeg -version.
const versionTimeout = 5 * time.Second

// FFmpegVersion runs path -version and returns the version it reports, such
// as 7.1.1.
func FFmpegVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-version")
	sysproc.NoConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return parseFFmpegVersion(string(out)), nil
}

// parseFFmpegVersion reads "ffmpeg version 7.1.1 Copyright …", the first line
// of ffmpeg -version, and returns the third word.
func parseFFmpegVersion(out string) string {
	line, _, _ := strings.Cut(out, "\n")
	if f := strings.Fields(line); len(f) >= 3 && f[1] == "version" {
		return f[2]
	}
	return ""
}
