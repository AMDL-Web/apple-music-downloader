package wrapperhost

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

// NativeRunner is Installed.Runner for a manager that runs as a process of
// this machine instead of in a QEMU guest. The guest is a Linux x86_64 VM, so
// on a Linux x86_64 host whose kernel allows unprivileged user namespaces,
// which wrapper-lite-rootless needs, the guest's own manager binary runs here
// directly.
const NativeRunner = "native"

// NativeAccel is State.Accel for a native manager: there is no emulator.
const NativeAccel = "native"

// managerName is the manager binary at the root of the guest's initramfs
// (manager/guest/build.sh in GuestRepo).
const managerName = "wrapper-manager"

// nativeDir is the native manager's working directory: it keeps its
// registry, accounts and the wrapper-lite payload in data/ under it. Like
// data.img it holds the accounts, so only a purge removes it.
const nativeDir = "native"

// NativeHost reports whether this machine can run the manager natively:
// Linux on x86_64 with unprivileged user namespaces. It starts a short-lived
// process to find out.
func NativeHost(goos, goarch string) bool {
	return goos == "linux" && goarch == "amd64" && userNamespaces() == nil
}

// NativeNext reports whether an install now gives a native manager: the host
// can run one and no guest disk holds accounts. An install that runs in QEMU
// keeps doing so, so its accounts stay where they are.
func (h Home) NativeNext(host bool) bool {
	if !host {
		return false
	}
	if in, err := h.Current(); err == nil && in.Runner != NativeRunner && in.Kind == ManagerGuest {
		return false
	}
	_, err := os.Stat(h.ImagePath())
	return errors.Is(err, os.ErrNotExist)
}

// Native reports whether in runs as a process of this machine.
func (in Installed) Native() bool { return in.Runner == NativeRunner }

// managerPath is the native manager binary of in.
func (h Home) managerPath(in Installed) string { return h.path("assets", in.ID, managerName) }

// extractManager copies the manager binary out of the gzipped newc cpio
// archive at initrd into dst.
func extractManager(initrd, dst string) error {
	f, err := os.Open(initrd)
	if err != nil {
		return err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s: %w", initrd, err)
	}
	r := bufio.NewReader(z)
	for {
		name, size, mode, err := cpioHeader(r)
		if err != nil {
			return fmt.Errorf("%s: %w", initrd, err)
		}
		if name == "TRAILER!!!" {
			return fmt.Errorf("%s has no %s", initrd, managerName)
		}
		if path.Clean(strings.TrimPrefix(name, "./")) == managerName && mode&0o170000 == 0o100000 {
			if size > unpackLimit {
				return fmt.Errorf("%s in %s is larger than %d MB", managerName, initrd, unpackLimit>>20)
			}
			return writeFile(io.LimitReader(r, size), dst, 0o700)
		}
		if _, err := io.CopyN(io.Discard, r, pad4(size)); err != nil {
			return fmt.Errorf("%s: %w", initrd, err)
		}
	}
}

// cpioHeader reads one newc header and its name, leaving r at the entry's
// data.
func cpioHeader(r *bufio.Reader) (name string, size int64, mode uint32, err error) {
	var hdr [110]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return "", 0, 0, err
	}
	if string(hdr[:6]) != "070701" && string(hdr[:6]) != "070702" {
		return "", 0, 0, errors.New("not a newc cpio archive")
	}
	field := func(i int) (uint64, error) { return strconv.ParseUint(string(hdr[6+8*i:14+8*i]), 16, 32) }
	m, err1 := field(1)
	sz, err2 := field(6)
	n, err3 := field(11)
	if err := errors.Join(err1, err2, err3); err != nil || n == 0 || n > 4096 {
		return "", 0, 0, errors.New("bad cpio header")
	}
	// The name and its NUL end where header and name together fill 4 bytes.
	raw := make([]byte, pad4(110+int64(n))-110)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", 0, 0, err
	}
	return string(raw[:n-1]), int64(sz), uint32(m), nil
}

func pad4(n int64) int64 { return (n + 3) &^ 3 }

// managerCommand runs the native manager on the loopback port, installing the
// pinned payload from payloadURL.
func (h Home) managerCommand(in Installed, payloadURL string) *exec.Cmd {
	cmd := exec.Command(h.managerPath(in), "-host", "127.0.0.1", "-port", strconv.Itoa(in.Port),
		"-payload-url", payloadURL, "-payload-sha256", in.Lite.SHA256)
	cmd.Dir = h.path(nativeDir)
	return cmd
}
