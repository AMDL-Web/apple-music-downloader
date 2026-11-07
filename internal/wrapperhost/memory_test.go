package wrapperhost

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGuestMemoryScalesAndCaps(t *testing.T) {
	for _, tc := range []struct{ accounts, want int }{{-1, 384}, {0, 384}, {1, 480}, {3, 672}, {10, 1344}, {11, 1344}} {
		t.Run(fmt.Sprint(tc.accounts), func(t *testing.T) {
			if got := GuestMemoryMB(tc.accounts); got != tc.want {
				t.Fatalf("memory %d MB, want %d MB", got, tc.want)
			}
		})
	}
}

func TestRegistryCountIncludesDegradedAccounts(t *testing.T) {
	n, err := registryCount([]byte(`[{"id":"one","region":"us"},{"id":"two","region":""},{"id":"three","degraded":true}]`))
	if err != nil || n != 3 {
		t.Fatalf("registry count %d, %v", n, err)
	}
	if n, err := registryCount([]byte(`[]`)); err != nil || n != 0 {
		t.Fatalf("empty registry %d, %v", n, err)
	}
	if _, err := registryCount([]byte(`not JSON`)); err == nil {
		t.Fatal("corrupt registry accepted")
	}
}

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
