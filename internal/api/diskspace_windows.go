package api

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// diskSpace returns the bytes available to the backend's user and the total
// size of the volume that holds path.
func diskSpace(path string) (free, total uint64, err error) {
	if path == "" {
		return 0, 0, errors.New("no directory configured")
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, size, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &size, &all); err != nil {
		return 0, 0, fmt.Errorf("GetDiskFreeSpaceEx %s: %w", path, err)
	}
	return avail, size, nil
}
