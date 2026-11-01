package usertoken

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	// StoreDirName is the directory, next to the database file, that holds
	// amdl's copy of the token GET /token returned. It is created 0700.
	StoreDirName = "usertoken"
	// StoreFileName is that copy, written 0600 by a rename.
	StoreFileName = "wrapper-token.json"
	maxStoreRead  = 64 << 10
)

// StoreDir is the directory amdl keeps its copy of the wrapper's token in,
// for a database at dbPath.
func StoreDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), StoreDirName)
}

// storedToken is the stored copy. Address is the wrapper.address it came
// from: a copy from another wrapper is not used. Times are Unix seconds.
type storedToken struct {
	Address       string `json:"address"`
	Region        string `json:"region"`
	MusicToken    string `json:"music_token"`
	RefreshedAt   int64  `json:"refreshed_at"`
	NextRefreshAt int64  `json:"next_refresh_at"`
	FetchedAt     int64  `json:"fetched_at"`
}

type tokenStore struct {
	dir string
}

func (s tokenStore) path() string { return filepath.Join(s.dir, StoreFileName) }

// load returns the stored copy for address, validated; "" with a nil error
// when there is none or it belongs to another address. Errors never include
// the file's contents.
func (s tokenStore) load(address string) (string, error) {
	f, err := os.Open(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxStoreRead+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", s.path(), err)
	}
	if len(raw) > maxStoreRead {
		return "", fmt.Errorf("%s: %w: longer than %d bytes", s.path(), ErrInvalid, maxStoreRead)
	}
	var st storedToken
	if json.Unmarshal(raw, &st) != nil {
		return "", fmt.Errorf("%s: %w: not the stored copy's JSON", s.path(), ErrInvalid)
	}
	if st.Address != address {
		return "", nil
	}
	v, err := Validate(st.MusicToken)
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.path(), err)
	}
	return v, nil
}

// save writes st to a temporary file in the store directory, mode 0600, and
// renames it over the copy, so a reader sees the old copy or the new one.
func (s tokenStore) save(st storedToken) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	// MkdirAll leaves an existing directory's mode alone.
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, StoreFileName+".*.tmp")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			// The write already failed; the leftover temp file is the only
			// thing to clean up and its own error adds nothing.
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.path()); err != nil {
		return err
	}
	done = true
	return nil
}

// drop removes the stored copy; a missing one is not an error.
func (s tokenStore) drop() error {
	err := os.Remove(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
