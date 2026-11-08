//go:build linux || darwin || freebsd

package api

import (
	"errors"
	"fmt"
	"syscall"
)

// diskSpace returns the bytes available to an unprivileged user and the total
// size of the file system that holds path. The Statfs_t field types differ
// per platform (Bsize is uint32 on darwin, int64 on linux/amd64), hence the
// conversions; this file builds only where those fields exist under these
// names.
func diskSpace(path string) (free, total uint64, err error) {
	if path == "" {
		return 0, 0, errors.New("no directory configured")
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	bsize := uint64(st.Bsize)
	return uint64(st.Bavail) * bsize, uint64(st.Blocks) * bsize, nil
}
