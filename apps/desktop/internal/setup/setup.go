// Package setup checks what each mode needs and installs what standalone
// mode needs on this computer: ffmpeg and QEMU with the platform's package
// manager, after the user agrees to the exact command, and the wrapper guest
// from upstream's artifact (internal/wrapperhost). The Setup's fields belong
// to the UI thread; background work hands its results back through post.
package setup

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"amdl/apps/desktop/internal/ffmpeg"
	"amdl/internal/wrapperhost"
)

// State is where a requirement stands.
type State int

// The states, from not looked at to met.
const (
	Unchecked State = iota
	Checking
	// Missing needs an action: an install, a download, a sign-in.
	Missing
	// Working is an action under way.
	Working
	Failed
	Ready
)

// Install is how to put a missing tool on this computer.
type Install struct {
	// Command is what runs, exactly as the user is shown it.
	Command string
	// Args are the program, by its full path, and its arguments, set when
	// the app may run Command for the user once they agree: a package
	// manager that needs no sudo (Homebrew, winget).
	Args []string
	// URL is where to get the package manager or the tool when the app
	// cannot install it.
	URL string
}

// Tool is ffmpeg or QEMU on this computer.
type Tool struct {
	State State
	Path  string
	// Source says where ffmpeg was found: built in, system or settings.
	Source  string
	Err     string
	Install Install
}

// Deps are what Setup reaches outside the process through, replaced in
// tests.
type Deps struct {
	GOOS string
	// LookPath finds a program on PATH; Exists reports a file.
	LookPath func(string) (string, error)
	Exists   func(string) bool
	// FFmpeg finds ffmpeg as the app runs it, from the settings' override.
	FFmpeg func(override string) (path, source string, err error)
	Home   wrapperhost.Home
	// Pin names the guest download; Fetcher fetches it.
	Pin     func(context.Context) (wrapperhost.Pin, error)
	Fetcher wrapperhost.Fetcher
	// Exec runs args to the end, writing what it prints to out.
	Exec func(ctx context.Context, args []string, out func(string)) error
	Now  func() time.Time
}

// Setup is the state of this computer's requirements and of what installs
// them.
type Setup struct {
	post func(func())
	deps Deps

	FFmpeg Tool
	QEMU   Tool
	Guest  Guest
	// Run is the package manager command running or last run, or nil.
	Run *Run
	// Server is the last test of a remote amdl.
	Server Server

	checked time.Time
}

// New returns a Setup that hands background results to post, which runs a
// function on the UI thread.
func New(post func(func()), deps Deps) *Setup {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Setup{post: post, deps: deps}
}

// SystemDeps are the real dependencies: this computer's files and programs,
// with the app's resources for the built-in ffmpeg and home for the guest.
func SystemDeps(resources string, home wrapperhost.Home, fetcher wrapperhost.Fetcher, pin func(context.Context) (wrapperhost.Pin, error)) Deps {
	return Deps{
		GOOS:     runtime.GOOS,
		LookPath: exec.LookPath,
		Exists:   exists,
		FFmpeg: func(override string) (string, string, error) {
			p, src, err := ffmpeg.Find(override, resources)
			return p, src.String(), err
		},
		Home:    home,
		Pin:     pin,
		Fetcher: fetcher,
		Exec:    execCommand,
		Now:     time.Now,
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// recheckEvery spaces the checks Check runs when asked again: a tool
// installed in a terminal shows within this.
const recheckEvery = 2 * time.Second

// Check looks again for ffmpeg, QEMU and the guest, unless it did within
// recheckEvery: the files are on this computer, so it answers at once. An
// action under way keeps its state.
func (s *Setup) Check(ffmpegOverride string) {
	now := s.deps.Now()
	if !s.checked.IsZero() && now.Sub(s.checked) < recheckEvery {
		return
	}
	s.checked = now
	if !s.running("ffmpeg") {
		s.FFmpeg = s.findFFmpeg(ffmpegOverride)
	}
	if !s.running("QEMU") {
		s.QEMU = s.findQEMU()
	}
	if s.Guest.State != Working {
		s.checkGuest()
	}
}

// Recheck makes the next Check look again at once.
func (s *Setup) Recheck() { s.checked = time.Time{} }

func (s *Setup) running(tool string) bool {
	return s.Run != nil && s.Run.Tool == tool && s.Run.State == Working
}

func (s *Setup) findFFmpeg(override string) Tool {
	p, src, err := s.deps.FFmpeg(override)
	if err == nil {
		return Tool{State: Ready, Path: p, Source: src}
	}
	t := Tool{State: Missing, Install: s.ffmpegInstall()}
	if override != "" {
		t.Err = err.Error()
	}
	return t
}

func (s *Setup) findQEMU() Tool {
	configured := ""
	if in, err := s.deps.Home.Current(); err == nil {
		configured = in.QEMU
	}
	p, err := wrapperhost.FindQEMU(configured, s.deps.GOOS, s.deps.LookPath, s.deps.Exists)
	if err == nil {
		return Tool{State: Ready, Path: p}
	}
	cmd, runnable := wrapperhost.QEMUInstall(s.deps.GOOS, s.has)
	t := Tool{State: Missing, Install: s.install(cmd, runnable)}
	if configured != "" {
		t.Err = err.Error()
	}
	return t
}

// managerDirs are where package managers put themselves, tried after PATH:
// an app opened from Finder gets launchd's PATH, which holds none of them.
var managerDirs = map[string][]string{
	"darwin": {"/opt/homebrew/bin", "/usr/local/bin"},
}

// find returns a program's full path, from PATH or managerDirs.
func (s *Setup) find(name string) (string, bool) {
	if p, err := s.deps.LookPath(name); err == nil {
		return p, true
	}
	for _, d := range managerDirs[s.deps.GOOS] {
		if p := filepath.Join(d, name); s.deps.Exists(p) {
			return p, true
		}
	}
	return "", false
}

func (s *Setup) has(name string) bool {
	_, ok := s.find(name)
	return ok
}

// wingetQuiet keeps winget from waiting on a prompt no one sees: the user
// agreed to the command they were shown, which includes these.
const wingetQuiet = " --accept-package-agreements --accept-source-agreements --disable-interactivity"

var firstURL = regexp.MustCompile(`https://\S+`)

// install completes a package manager's command: the app runs it by the
// manager's full path, and winget without prompts.
func (s *Setup) install(cmd string, runnable bool) Install {
	in := Install{Command: cmd}
	if !runnable {
		in.URL = strings.TrimRight(firstURL.FindString(cmd), ",.")
		return in
	}
	if strings.HasPrefix(cmd, "winget ") {
		cmd += wingetQuiet
		in.Command = cmd
	}
	args := strings.Fields(cmd)
	if p, ok := s.find(args[0]); ok {
		args[0] = p
		in.Args = args
	}
	return in
}

// ffmpegInstall is how to install ffmpeg on this computer, after
// wrapperhost.QEMUInstall's choice of package managers.
func (s *Setup) ffmpegInstall() Install {
	switch s.deps.GOOS {
	case "darwin":
		if s.has("brew") {
			return s.install("brew install ffmpeg", true)
		}
		return s.install("install Homebrew from https://brew.sh, then brew install ffmpeg", false)
	case "windows":
		if s.has("winget") {
			return s.install("winget install --id Gyan.FFmpeg -e", true)
		}
		return s.install("install ffmpeg from https://ffmpeg.org/download.html", false)
	}
	for _, w := range []struct{ manager, cmd string }{
		{"apt-get", "sudo apt-get install ffmpeg"},
		{"dnf", "sudo dnf install ffmpeg"},
		{"pacman", "sudo pacman -S ffmpeg"},
		{"apk", "sudo apk add ffmpeg"},
	} {
		if s.has(w.manager) {
			return s.install(w.cmd, false)
		}
	}
	return s.install("install ffmpeg from https://ffmpeg.org/download.html", false)
}
