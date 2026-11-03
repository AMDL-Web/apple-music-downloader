package wrapperhost

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// seedImage writes one of internal/ext4's test images as h's data.img.
func seedImage(t *testing.T, h Home, name string) {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "ext4", "testdata", name+".img.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	out, err := os.OpenFile(h.ImagePath(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	if _, err := io.Copy(out, z); err != nil {
		t.Fatal(err)
	}
}

func TestAccountCountOfAnEmptyExt4Registry(t *testing.T) {
	h := Home{Dir: t.TempDir()}
	seedImage(t, h, "nofile")
	if got, err := h.accountCount(); err != nil || got != 0 {
		t.Fatalf("empty registry count: %d, %v", got, err)
	}
}

func TestMusicTokenWithoutAnInstallation(t *testing.T) {
	h := Home{Dir: t.TempDir()}
	if got, err := h.MusicToken(); got != "" || err != nil {
		t.Fatalf("no installation: %d bytes, %v", len(got), err)
	}
}
