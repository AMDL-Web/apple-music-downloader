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

func TestFindWithoutAnyFFmpeg(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, _, err := Find("", t.TempDir()); err != ErrNotFound {
		t.Errorf("err = %v", err)
	}
}
