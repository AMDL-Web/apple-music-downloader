package wrapperhost

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrubLoginZeroesOnlyTheArguments(t *testing.T) {
	// The args file bootLoginLocked writes, as the guest copies it to disk.
	copy := []byte("--login\nme@example.com:p ss:w0rd\n--base-dir\n/data\n--host\n0.0.0.0\n--port\n8080\n")
	nearMiss := []byte("--login\nnot@example.com:x\n--base-dir\n/srv\n--host\n0.0.0.0\n--port\n8080\n")
	img := bytes.Repeat([]byte{0xAB}, 3<<20)
	at := []int{7, 1<<20 - 20, 2<<20 + 4093} // the second spans two chunks
	for _, o := range at {
		copyAt(img, o, copy)
	}
	missAt := 1<<20 + 5000
	copyAt(img, missAt, nearMiss)
	path := filepath.Join(t.TempDir(), "data.img")
	if err := os.WriteFile(path, img, 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := scrubLogin(path)
	if err != nil || n != len(at) {
		t.Fatalf("scrubLogin = %d, %v; want %d", n, err, len(at))
	}
	got, _ := os.ReadFile(path)
	want := append([]byte(nil), img...)
	for _, o := range at {
		clear(want[o : o+len(copy)])
	}
	if !bytes.Equal(got, want) {
		t.Fatal("scrubLogin changed more or less than the three copies")
	}
	if !bytes.Contains(got, nearMiss) || strings.Contains(string(got), "p ss:w0rd") {
		t.Fatal("the near miss was zeroed or a password survived")
	}
	if n, err := scrubLogin(path); err != nil || n != 0 {
		t.Fatalf("second scrub = %d, %v", n, err)
	}
}

func TestLoginArgsEndWithWhatScrubLooksFor(t *testing.T) {
	file := strings.Join(loginArgs("u", "p"), "\n") + "\n"
	if !strings.HasPrefix(file, "--login\nu:p\n") || !strings.HasSuffix(file, "\n--port\n8080\n") {
		t.Fatalf("login args file = %q", file)
	}
}

func copyAt(dst []byte, at int, src []byte) { copy(dst[at:], src) }
