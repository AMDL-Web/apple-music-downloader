package ffmpeg

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindPrefersOverrideThenBundled(t *testing.T) {
	res := t.TempDir()
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bundled := filepath.Join(res, "bin", name)
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundled, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if path, src, err := Find("", res); err != nil || src != Bundled || path != bundled {
		t.Errorf("bundled: %q %v %v", path, src, err)
	}
	if path, src, err := Find(bundled, ""); err != nil || src != Override || path != bundled {
		t.Errorf("override: %q %v %v", path, src, err)
	}
	if _, src, err := Find(filepath.Join(res, "missing"), res); err == nil || src != Override {
		t.Errorf("a missing override fell back: %v %v", src, err)
	}
}

// withPackageDirs makes Find look in dirs after PATH for the rest of the
// test.
func withPackageDirs(t *testing.T, dirs ...string) {
	prev := packageDirs
	packageDirs = dirs
	t.Cleanup(func() { packageDirs = prev })
}

func TestFindWithoutAnyFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	withPackageDirs(t)
	if _, _, err := Find("", t.TempDir()); err != ErrNotFound {
		t.Errorf("err = %v", err)
	}
}

// An app opened from Finder has none of Homebrew's directories on PATH.
func TestFindInPackageDirOffPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("package directories are a macOS fallback")
	}
	t.Setenv("PATH", t.TempDir())
	empty, brew := t.TempDir(), t.TempDir()
	want := filepath.Join(brew, "ffmpeg")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	withPackageDirs(t, empty, brew)
	if path, src, err := Find("", t.TempDir()); err != nil || src != System || path != want {
		t.Errorf("Find = %q %v %v, want %q from the package directory", path, src, err, want)
	}
}
