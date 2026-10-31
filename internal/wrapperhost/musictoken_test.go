package wrapperhost

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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

func TestMusicToken(t *testing.T) {
	h := Home{Dir: t.TempDir()}
	if got, err := h.MusicToken(); got != "" || err != nil {
		t.Fatalf("no disk: %q, %v", got, err)
	}
	seedImage(t, h, "nofile")
	if got, err := h.MusicToken(); got != "" || err != nil {
		t.Fatalf("no MUSIC_TOKEN: %q, %v", got, err)
	}
	seedImage(t, h, "std4k")
	got, err := h.MusicToken()
	sum := sha256.Sum256([]byte(got))
	if err != nil || hex.EncodeToString(sum[:]) != "5c0c8438c1e7f56a6dbb654912c95188d3704fcd2540ba3db3a14bfec78a55bb" {
		t.Fatalf("MusicToken: %d bytes, %v; want the test image's made-up token", len(got), err)
	}
	if err := os.WriteFile(h.ImagePath(), make([]byte, 1<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.MusicToken(); err == nil {
		t.Fatal("a disk that is not ext4 reads without an error")
	}
}
