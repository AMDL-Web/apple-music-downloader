//go:build !(linux || darwin || freebsd)

package api

import (
	"errors"
	"runtime"
)

// diskSpace is not implemented on this platform; GET /system reports the
// error string in place of the byte counts.
func diskSpace(string) (free, total uint64, err error) {
	return 0, 0, errors.New("statfs unsupported on " + runtime.GOOS)
}
