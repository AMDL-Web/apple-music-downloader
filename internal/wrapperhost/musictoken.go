package wrapperhost

import (
	"errors"
	"io/fs"
	"os"

	"amdl/internal/ext4"
)

// maxMusicToken bounds the MUSIC_TOKEN read; Apple's tokens are a few hundred
// bytes.
const maxMusicToken = 4096

// MusicToken returns the raw contents of the MUSIC_TOKEN file lite keeps at
// the root of the account disk, or "" when there is no disk or no such file.
// The guest may be running: its kernel writes a new token back to the disk
// within about half a minute, and until then this reads the previous one,
// which keeps working. A torn read of a disk being written is caught by the
// caller's validation.
func (h Home) MusicToken() (string, error) {
	f, err := os.Open(h.ImagePath())
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	raw, err := ext4.ReadFile(f, st.Size(), "/MUSIC_TOKEN", maxMusicToken)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
