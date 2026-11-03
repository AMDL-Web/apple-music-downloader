package wrapper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// pairingStore is the stored credential.
type pairingStore struct{ dir string }

const pairingFile = "wrapper.json"

func (s pairingStore) path() string { return filepath.Join(s.dir, pairingFile) }

func (s pairingStore) load(address string) (*pairingCredential, error) {
	if s.dir == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cred pairingCredential
	if json.Unmarshal(raw, &cred) != nil || cred.AccessToken == "" || cred.RefreshToken == "" {
		return nil, fmt.Errorf("%s is not a stored pairing", s.path())
	}
	if cred.Address != address {
		return nil, nil
	}
	return &cred, nil
}

func (s pairingStore) save(cred pairingCredential) error {
	if s.dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, pairingFile+".*.tmp")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
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

func (s pairingStore) drop() error {
	if s.dir == "" {
		return nil
	}
	err := os.Remove(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
