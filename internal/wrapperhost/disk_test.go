package wrapperhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func legacyHome(t *testing.T) Home {
	t.Helper()
	h := Home{Dir: t.TempDir()}
	if err := h.prepare(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.ImagePath(), []byte("synthetic legacy account state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(h.path("installed.json"), Installed{ID: "old-lite", Port: 12345, QEMU: "configured-qemu"}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestLegacyDiskIsPreservedAndManagerUpdatesKeepAllAccounts(t *testing.T) {
	h := legacyHome(t)
	art := fakeArtifact(t, "", "fresh manager disk")
	in, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "manager-first"), false, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != ManagerGuest || in.LegacyDisk == "" || in.Port != 12345 || in.QEMU != "configured-qemu" {
		t.Fatalf("installed metadata: %+v", in)
	}
	backup := filepath.Join(h.Dir, in.LegacyDisk)
	got, err := os.ReadFile(backup)
	if err != nil || string(got) != "synthetic legacy account state" {
		t.Fatal("legacy account disk was not preserved verbatim")
	}
	if info, err := os.Stat(backup); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("legacy backup is not owner-only")
	}
	got, _ = os.ReadFile(h.ImagePath())
	if string(got) != "fresh manager disk" {
		t.Fatal("manager reused the legacy disk")
	}
	os.WriteFile(h.ImagePath(), []byte("all manager accounts"), 0o600)
	if err := h.Uninstall(false); err != nil {
		t.Fatal(err)
	}
	in, err = h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "manager-next"), false, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(h.ImagePath())
	if string(got) != "all manager accounts" || in.LegacyDisk != filepath.Base(backup) {
		t.Fatal("reinstall replaced a manager disk or forgot the legacy backup")
	}
	if err := h.Uninstall(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("explicit purge kept the legacy backup")
	}
}

func TestLegacyDiskCannotBeReplacedWhileGuestRuns(t *testing.T) {
	h := legacyHome(t)
	writeJSON(h.path("state.json"), State{Mode: "serving", PID: 123})
	art := fakeArtifact(t, "", "fresh disk")
	_, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "manager-first"), false, time.Now(), nil)
	if err == nil || !strings.Contains(err.Error(), "stop the local wrapper") {
		t.Fatalf("running legacy guest update: %v", err)
	}
	got, _ := os.ReadFile(h.ImagePath())
	if string(got) != "synthetic legacy account state" {
		t.Fatal("running disk was changed")
	}
}

func TestImportBareLiteDoesNotMoveLegacyDisk(t *testing.T) {
	h := legacyHome(t)
	dir := t.TempDir()
	for _, name := range []string{"vmlinuz-lite-qemu", "lite-initramfs.cpio.gz", "data.img"} {
		os.WriteFile(filepath.Join(dir, name), []byte("old guest"), 0o600)
	}
	if _, err := h.Import(dir, false, time.Now()); err == nil {
		t.Fatal("bare-lite guest was accepted")
	}
	got, _ := os.ReadFile(h.ImagePath())
	if string(got) != "synthetic legacy account state" {
		t.Fatal("rejected import touched account disk")
	}
}

func TestInterruptedManagerDiskSeedingKeepsLegacyBackup(t *testing.T) {
	h := legacyHome(t)
	backup := "data-lite-interrupted.img"
	if err := os.Rename(h.ImagePath(), h.path(backup)); err != nil {
		t.Fatal(err)
	}
	writeJSON(h.path("disk.json"), diskLayout{Kind: ManagerGuest, LegacyDisk: backup})
	art := fakeArtifact(t, "", "fresh disk")
	in, err := h.Install(context.Background(), &fakeFetcher{body: art}, pinFor(art, "manager-first"), false, time.Now(), nil)
	if err != nil || in.LegacyDisk != backup {
		t.Fatalf("resumed install: %+v, %v", in, err)
	}
	got, _ := os.ReadFile(h.path(backup))
	if string(got) != "synthetic legacy account state" {
		t.Fatal("resumed install lost legacy state")
	}
}
