package wrapperhost

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newc writes a gzipped newc cpio archive of entries, in order, as
// manager/guest/build.sh's cpio -o -H newc does.
func newc(t *testing.T, entries ...[3]string) []byte {
	t.Helper()
	var raw bytes.Buffer
	pad := func() {
		for raw.Len()%4 != 0 {
			raw.WriteByte(0)
		}
	}
	write := func(name string, mode uint32, body string) {
		fmt.Fprintf(&raw, "070701%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X%08X",
			1, mode, 0, 0, 1, 0, len(body), 0, 0, 0, 0, len(name)+1, 0)
		raw.WriteString(name)
		raw.WriteByte(0)
		pad()
		raw.WriteString(body)
		pad()
	}
	for _, e := range entries {
		mode, _ := strconv.ParseUint(e[1], 8, 32)
		write(e[0], uint32(mode), e[2])
	}
	write("TRAILER!!!", 0, "")
	var z bytes.Buffer
	w := gzip.NewWriter(&z)
	w.Write(raw.Bytes())
	w.Close()
	return z.Bytes()
}

// nativeArtifact is a guest zip whose initramfs holds, beside other files, a
// manager that runs this test binary as fakeManager.
func nativeArtifact(t *testing.T) []byte {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nAMDL_FAKE_MANAGER=1 exec '" + exe + "' \"$@\"\n"
	initrd := newc(t,
		[3]string{"bin", "040755", ""},
		[3]string{"init", "100755", "#!/bin/sh\n"},
		[3]string{"e1000.ko", "100644", "odd-sized module"},
		[3]string{"wrapper-manager", "100755", script},
	)
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range map[string][]byte{kernelName: []byte("kernel"), initrdName: initrd, imageName: []byte("[]")} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(body)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestExtractManagerFindsItAmongOtherEntries(t *testing.T) {
	dir := t.TempDir()
	initrd := filepath.Join(dir, initrdName)
	os.WriteFile(initrd, newc(t,
		[3]string{"wrapper-manager.d", "040755", ""},
		[3]string{"a", "100644", "x"},
		[3]string{"./wrapper-manager", "100755", "the manager"},
	), 0o600)
	dst := filepath.Join(dir, "out")
	if err := extractManager(initrd, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "the manager" {
		t.Errorf("extracted %q", got)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm()&0o100 == 0 {
		t.Errorf("mode %v is not executable", st.Mode())
	}
	os.WriteFile(initrd, newc(t, [3]string{"init", "100755", "x"}), 0o600)
	if err := extractManager(initrd, dst+"2"); err == nil || !strings.Contains(err.Error(), "has no wrapper-manager") {
		t.Errorf("no manager: %v", err)
	}
}

func TestNativeNextKeepsAGuestWithAccountsInQEMU(t *testing.T) {
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	if h.NativeNext(false) {
		t.Error("a host without user namespaces runs QEMU")
	}
	if !h.NativeNext(true) {
		t.Error("a fresh install on a capable host runs natively")
	}
	art := fakeArtifact(t, "", "[]")
	if _, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "bde3335f47a30518"), false, time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if h.NativeNext(true) {
		t.Error("an installed guest keeps its disk in QEMU")
	}
}

func TestNativeManagerRunsWithoutQEMU(t *testing.T) {
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	art := nativeArtifact(t)
	in, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "bde3335f47a30518"), true, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Native() || in.QEMU != "" || in.Port == 0 {
		t.Fatalf("installed %+v", in)
	}
	if _, err := os.Stat(h.ImagePath()); !os.IsNotExist(err) {
		t.Errorf("a native install made a guest disk: %v", err)
	}
	if !h.NativeNext(true) {
		t.Error("a native install updates natively")
	}
	s, err := NewSupervisor(h, "/nonexistent/qemu", "kvm", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	s.fetcher = &fakeFetcher{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	waitMode(t, s, "serving")
	if m := s.Managed(); m.Accel != NativeAccel || m.QEMU != "" {
		t.Errorf("managed %+v, want accel native and no QEMU", m)
	}
	work := filepath.Join(h.Dir, nativeDir)
	if got, _ := os.ReadFile(filepath.Join(work, "payload.sha256")); string(got) != fakeLite.SHA256 {
		t.Errorf("the manager installed payload %q, want %s", got, fakeLite.SHA256)
	}
	if args, _ := os.ReadFile(filepath.Join(work, "args")); !strings.Contains(string(args), "-host\n127.0.0.1\n-port\n"+strconv.Itoa(in.Port)) {
		t.Errorf("manager args %q", args)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if _, err := os.Stat(filepath.Join(work, "terminated")); err != nil {
		t.Error("the manager was not asked to stop with SIGTERM")
	}

	// Accounts live in the working directory, and the port stays, across
	// an uninstall that keeps them.
	os.WriteFile(filepath.Join(work, "accounts"), nil, 0o600)
	if err := h.Uninstall(false); err != nil {
		t.Fatal(err)
	}
	again, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "0123456789abcdef"), h.NativeNext(true), time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Native() || again.Port != in.Port {
		t.Errorf("reinstalled %+v, want native on port %d", again, in.Port)
	}
	if _, err := os.Stat(filepath.Join(work, "accounts")); err != nil {
		t.Error("uninstall without purge removed the accounts")
	}
	if err := h.Uninstall(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Error("purge kept the accounts")
	}
}
