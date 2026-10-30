package wrapperhost

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Install downloads p's qemu-assets artifact with f, verifies it and makes it
// the installed guest. An account's data.img is kept; a first install seeds it
// from the artifact's empty image.
func (h Home) Install(ctx context.Context, f Fetcher, p Pin, now time.Time) (Installed, error) {
	if err := h.prepare(); err != nil {
		return Installed{}, err
	}
	zipPath := h.path("download-" + p.SHA256[:12] + ".zip")
	if err := Download(ctx, f, p, zipPath); err != nil {
		return Installed{}, err
	}
	in, err := h.unpack(zipPath, Installed{ID: p.Commit[:min(12, len(p.Commit))], Commit: p.Commit,
		ArtifactID: p.ArtifactID, SHA256: p.SHA256, Source: fmt.Sprintf("artifact %d", p.ArtifactID), At: now})
	if err == nil {
		_ = os.Remove(zipPath) // unpacked; a leftover only costs disk
	}
	return in, err
}

// Import installs a qemu-assets zip, one of upstream's wrapper-lite-qemu
// packages, or a directory holding vmlinuz-lite-qemu, lite-initramfs.cpio.gz
// and data.img: the way in when no pin can be downloaded any more.
func (h Home) Import(path string, now time.Time) (Installed, error) {
	if err := h.prepare(); err != nil {
		return Installed{}, err
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return Installed{}, err
	}
	return h.unpack(path, Installed{ID: "import-" + sum[:12], SHA256: sum, Source: "import of " + filepath.Base(path), At: now})
}

func (h Home) prepare() error {
	for _, d := range []string{h.Dir, h.path("assets"), h.path("logs")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return os.Chmod(h.Dir, 0o700)
}

// unpackLimit caps what one guest file may expand to.
const unpackLimit = 512 << 20

// unpack copies the guest files out of src, a zip or a directory, into
// assets/<id>, seeds data.img if there is none, records in as installed and
// removes the assets it replaced.
func (h Home) unpack(src string, in Installed) (Installed, error) {
	dst := h.path("assets", in.ID)
	tmp := dst + ".tmp"
	_ = os.RemoveAll(tmp) // a failed earlier attempt
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return Installed{}, err
	}
	want := map[string]bool{kernelName: true, initrdName: true, imageName: true}
	found, err := copyOut(src, want, tmp)
	if err != nil {
		return Installed{}, err
	}
	for name := range want {
		if !found[name] {
			return Installed{}, fmt.Errorf("%s has no %s; expected upstream's qemu-assets artifact or a wrapper-lite-qemu package", src, name)
		}
	}
	_ = os.RemoveAll(dst) // a reinstall of the same id
	if err := os.Rename(tmp, dst); err != nil {
		return Installed{}, err
	}
	if _, err := os.Stat(h.ImagePath()); errors.Is(err, os.ErrNotExist) {
		if err := copyFile(filepath.Join(dst, imageName), h.ImagePath(), 0o600); err != nil {
			return Installed{}, err
		}
	}
	if prev, err := h.Current(); err == nil {
		in.Port = prev.Port
		if in.QEMU == "" {
			in.QEMU = prev.QEMU
		}
	}
	if in.Port == 0 {
		if in.Port, err = freePort(); err != nil {
			return Installed{}, err
		}
	}
	if err := writeJSON(h.path("installed.json"), in); err != nil {
		return Installed{}, err
	}
	entries, _ := os.ReadDir(h.path("assets")) // listing only prunes; a failure leaves old files
	for _, e := range entries {
		if e.Name() != in.ID {
			_ = os.RemoveAll(h.path("assets", e.Name())) // an older guest no longer in use
		}
	}
	return in, nil
}

// copyOut copies the wanted files, matched by base name at any depth, from a
// zip or a directory into dir.
func copyOut(src string, want map[string]bool, dir string) (map[string]bool, error) {
	found := map[string]bool{}
	st, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !want[d.Name()] || found[d.Name()] {
				return err
			}
			found[d.Name()] = true
			return copyFile(p, filepath.Join(dir, d.Name()), 0o600)
		})
		return found, err
	}
	z, err := zip.OpenReader(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	defer z.Close()
	for _, f := range z.File {
		name := filepath.Base(f.Name)
		if !want[name] || found[name] || f.FileInfo().IsDir() {
			continue
		}
		if f.UncompressedSize64 > unpackLimit {
			return nil, fmt.Errorf("%s in %s is larger than %d MB", f.Name, src, unpackLimit>>20)
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		err = writeFile(io.LimitReader(r, unpackLimit), filepath.Join(dir, name), 0o600)
		r.Close()
		if err != nil {
			return nil, err
		}
		found[name] = true
	}
	return found, nil
}

// Uninstall removes the installed guest. With purge it also removes data.img,
// which logs the account out for good.
func (h Home) Uninstall(purge bool) error {
	for _, p := range []string{h.path("installed.json"), h.path("assets"), h.path("state.json")} {
		if err := os.RemoveAll(p); err != nil {
			return err
		}
	}
	if purge {
		return os.RemoveAll(h.Dir)
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeFile(in, dst, perm)
}

func writeFile(r io.Reader, dst string, perm os.FileMode) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func fileSHA256(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if st.IsDir() {
		_, _ = h.Write([]byte(path)) // a directory is named by its path
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}
