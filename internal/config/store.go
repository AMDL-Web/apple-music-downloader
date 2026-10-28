package config

import (
	"errors"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
)

// Store holds the live configuration shared by every component that must
// observe runtime changes after startup. Get returns the current snapshot by
// value; callers must treat slices inside it as read-only, because mutating
// them in place would race with concurrent readers of the same snapshot.
//
// A Store created with NewFileStore is backed by config.yaml: Reload applies
// hand edits of runtime-mutable fields, and UpdateAndSave persists API changes
// so they survive a restart. NewStore creates an in-memory Store for tests.
type Store struct {
	current atomic.Pointer[Config]
	// path is the backing config file; empty for in-memory stores. saveMu
	// serializes Reload and UpdateAndSave so a file read, a file write and a
	// snapshot swap never interleave with another one.
	path   string
	saveMu sync.Mutex
}

// NewStore returns an in-memory Store holding cfg.
func NewStore(cfg Config) *Store {
	s := &Store{}
	s.Set(cfg)
	return s
}

// NewFileStore returns a Store holding cfg and backed by the config file at
// path.
func NewFileStore(cfg Config, path string) *Store {
	s := NewStore(cfg)
	s.path = path
	return s
}

// Persistent reports whether updates are written back to config.yaml and
// therefore survive a restart.
func (s *Store) Persistent() bool {
	return s.path != ""
}

// Get returns the current snapshot.
func (s *Store) Get() Config {
	return *s.current.Load()
}

// Set replaces the snapshot without touching the backing file.
func (s *Store) Set(cfg Config) {
	s.current.Store(&cfg)
}

// UpdateAndSave applies mutate and persists the result. Under saveMu it first
// re-reads the backing file exactly as Reload does, so mutate starts from any
// hand edit made since the last reload instead of overwriting it; then it
// writes the runtime-mutable fields of mutate's result to the file (keeping
// startup-bound values on disk) and only then swaps the snapshot. An invalid
// file, a mutate error or a failed write leaves both the snapshot and the file
// unchanged. A missing file counts as having no hand edits and is recreated.
// Concurrent updates run one after another, each starting from the previous
// one's result.
func (s *Store) UpdateAndSave(mutate func(current Config) (Config, error)) (Config, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	current, err := s.readLocked(true)
	if err != nil {
		return Config{}, err
	}
	updated, err := mutate(current)
	if err != nil {
		return Config{}, err
	}
	if s.path != "" {
		if err := saveRuntimeFields(s.path, updated); err != nil {
			return Config{}, err
		}
	}
	s.Set(updated)
	return updated, nil
}

// Reload re-reads the backing file through Load (AMDL_* overrides included)
// and applies its runtime-mutable fields. Startup-bound fields keep their
// in-memory values, because components built from them cannot follow a live
// change; those edits apply on restart. A runtime key deleted from the file
// resets to its Default value. When the file cannot be read or is invalid, the
// previous snapshot stays in place and the error is returned. No-op for
// in-memory stores.
func (s *Store) Reload() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	cfg, err := s.readLocked(false)
	if err != nil {
		return err
	}
	s.Set(cfg)
	return nil
}

// readLocked returns the current snapshot with its runtime-mutable fields
// replaced by the backing file's; with missingOK, a file that does not exist
// yields the snapshot unchanged. The caller holds saveMu.
func (s *Store) readLocked(missingOK bool) (Config, error) {
	cfg := s.Get()
	if s.path == "" {
		return cfg, nil
	}
	loaded, err := load(s.path, os.Environ())
	if missingOK && errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	copyRuntimeFields(&cfg, loaded)
	return cfg, nil
}
