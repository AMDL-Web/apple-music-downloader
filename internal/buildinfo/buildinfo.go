// Package buildinfo reports which build of the backend is running: the
// version, commit and build date stamped at build time, and when the process
// started.
package buildinfo

import (
	"runtime/debug"
	"strings"
	"time"
)

// Version, Commit and BuildDate are stamped at build time with
//
//	-ldflags "-X amdl/internal/buildinfo.Version=v2.0.0
//	          -X amdl/internal/buildinfo.Commit=a6a6b46
//	          -X amdl/internal/buildinfo.BuildDate=2026-10-28T12:00:00Z"
//
// as the Makefile and the Dockerfile do. Read them through Get, which fills an
// unset Commit or BuildDate from the VCS stamp of the go command.
var (
	Version   = "dev"
	Commit    = ""
	BuildDate = ""
)

// StartedAt is when this process started, in UTC.
var StartedAt = time.Now().UTC()

// shortCommitLen is the length a VCS revision is cut to, matching the
// default of git rev-parse --short.
const shortCommitLen = 7

// Info is the build identity of the running binary.
type Info struct {
	// Version is the release tag (git describe output), or "dev" for a build
	// made without -ldflags.
	Version string
	// Commit is the abbreviated commit hash, empty when unknown.
	Commit string
	// BuildDate is an RFC 3339 UTC timestamp, empty when unknown. Without
	// -ldflags it is the commit time from the VCS stamp, not the build time.
	BuildDate string
}

// Get returns the stamped build info, falling back to the vcs.revision and
// vcs.time settings that go build records inside a git checkout. go run, go
// test and builds without a .git directory (the Docker build context) record
// no VCS settings, so there the -ldflags values are the only source. go build
// looks for a .git directory, not the .git file of a worktree, so in a
// worktree nested inside another checkout (.claude/worktrees/) it records the
// outer checkout's commit; the Makefile's -ldflags take precedence.
func Get() Info {
	var settings []debug.BuildSetting
	if info, ok := debug.ReadBuildInfo(); ok {
		settings = info.Settings
	}
	return resolve(Version, Commit, BuildDate, settings)
}

// resolve combines the -ldflags values with the VCS build settings; an
// explicitly stamped value always wins.
func resolve(version, commit, buildDate string, settings []debug.BuildSetting) Info {
	info := Info{
		Version:   strings.TrimSpace(version),
		Commit:    strings.TrimSpace(commit),
		BuildDate: strings.TrimSpace(buildDate),
	}
	if info.Version == "" {
		info.Version = "dev"
	}
	var revision, vcsTime string
	modified := false
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.time":
			vcsTime = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if info.Commit == "" && revision != "" {
		info.Commit = revision
		if len(info.Commit) > shortCommitLen {
			info.Commit = info.Commit[:shortCommitLen]
		}
		if modified {
			info.Commit += "-dirty"
		}
	}
	if info.BuildDate == "" {
		info.BuildDate = vcsTime
	}
	return info
}
