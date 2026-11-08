package buildinfo

import (
	"runtime/debug"
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "-trimpath", Value: "true"},
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "0ac19903c8b8d5c2a7e3f1b6d9a4e2c7b1f0d3a5"},
		{Key: "vcs.time", Value: "2026-10-27T09:30:00Z"},
		{Key: "vcs.modified", Value: "false"},
	}
	dirty := append(append([]debug.BuildSetting{}, vcs[:4]...), debug.BuildSetting{Key: "vcs.modified", Value: "true"})
	tests := []struct {
		name                       string
		version, commit, buildDate string
		settings                   []debug.BuildSetting
		want                       Info
	}{
		{
			name:     "no ldflags and no VCS stamp",
			version:  "dev",
			settings: []debug.BuildSetting{{Key: "-trimpath", Value: "true"}},
			want:     Info{Version: "dev"},
		},
		{
			name:     "no ldflags falls back to the VCS stamp",
			version:  "dev",
			settings: vcs,
			want:     Info{Version: "dev", Commit: "0ac1990", BuildDate: "2026-10-27T09:30:00Z"},
		},
		{
			name:     "uncommitted changes mark the fallback commit",
			version:  "dev",
			settings: dirty,
			want:     Info{Version: "dev", Commit: "0ac1990-dirty", BuildDate: "2026-10-27T09:30:00Z"},
		},
		{
			name:    "ldflags win over the VCS stamp",
			version: "v2.0.0", commit: "a6a6b46", buildDate: "2026-10-28T12:00:00Z",
			settings: vcs,
			want:     Info{Version: "v2.0.0", Commit: "a6a6b46", BuildDate: "2026-10-28T12:00:00Z"},
		},
		{
			name:    "only the unset fields fall back",
			version: "v2.0.0", commit: "a6a6b46",
			settings: vcs,
			want:     Info{Version: "v2.0.0", Commit: "a6a6b46", BuildDate: "2026-10-27T09:30:00Z"},
		},
		{
			name:     "an empty stamped version reads as dev",
			version:  " ",
			settings: nil,
			want:     Info{Version: "dev"},
		},
		{
			name:     "a short revision is kept whole",
			version:  "dev",
			settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc12"}},
			want:     Info{Version: "dev", Commit: "abc12"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.version, tc.commit, tc.buildDate, tc.settings); got != tc.want {
				t.Fatalf("resolve() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStartedAtIsProcessStart(t *testing.T) {
	if StartedAt.IsZero() || StartedAt.After(time.Now()) || StartedAt.Location() != time.UTC {
		t.Fatalf("StartedAt = %v, want a past UTC time", StartedAt)
	}
}

func TestGetDefaultsToDev(t *testing.T) {
	// go test builds carry no -ldflags stamp and no VCS settings.
	if got := Get(); got.Version != "dev" {
		t.Fatalf("Get().Version = %q, want dev", got.Version)
	}
}
