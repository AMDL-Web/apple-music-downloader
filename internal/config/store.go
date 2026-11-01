package config

import (
	"errors"
	"io/fs"
	"sync"
	"sync/atomic"
)

// Store holds the live configuration shared by every component that must
// observe runtime changes after startup. Get returns the current snapshot by
// value; callers must treat slices inside it as read-only, because mutating
// them in place would race with concurrent readers of the same snapshot.
//
// The snapshot always holds the values the process runs with: runtime keys
// as last applied, restart and file keys as read at startup. A Store created
// with NewFileStore is backed by config.yaml, which also holds saved values
// that wait for a restart: Reload applies hand edits of runtime keys,
// UpdateAndSave persists API changes so they survive a restart, and Saved
// and PendingRestart report what the next start will change. NewStore
// creates an in-memory Store for tests.
type Store struct {
	current atomic.Pointer[Config]
	// path is the backing config file; empty for in-memory stores. saveMu
	// serializes Reload, UpdateAndSave and Saved so a file read, a file write
	// and a snapshot swap never interleave with another one.
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

// Path is the config file updates are written to; "" for an in-memory store.
func (s *Store) Path() string {
	return s.path
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
// re-reads the backing file, so mutate starts from the runtime and restart
// values saved there (any hand edit made since the last reload, any change
// waiting for a restart) instead of overwriting them; then it writes the
// runtime and restart fields of mutate's result to the file (keeping
// file-class values on disk) and only then applies the runtime fields to the
// snapshot. It returns mutate's result with the file-class fields as the file
// holds them, the configuration as saved: pass it to PendingRestart, which
// then names a hand-edited file-only key as Saved does. An invalid file, a mutate error or a failed write leaves
// both the snapshot and the file unchanged. A missing file counts as having
// no hand edits and is recreated. Concurrent updates run one after another,
// each starting from the previous one's result.
func (s *Store) UpdateAndSave(mutate func(current Config) (Config, error)) (Config, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	live := s.Get()
	base, err := s.readLocked(live, func(string) bool { return true }, true)
	if err != nil {
		return Config{}, err
	}
	updated, err := mutate(base)
	if err != nil {
		return Config{}, err
	}
	copyFields(&updated, base, func(key string) bool { return !isEditable(key) })
	if s.path != "" {
		if err := saveEditableFields(s.path, updated); err != nil {
			return Config{}, err
		}
	}
	copyFields(&live, updated, isRuntime)
	s.Set(live)
	return updated, nil
}

// Reload re-reads the backing file and applies its runtime fields. Restart
// and file fields keep their in-memory values, because components built from
// them cannot follow a live change; those edits apply on restart. A runtime
// key deleted from the file resets to its Default value. When the file cannot
// be read or is invalid, the previous snapshot stays in place and the error
// is returned. No-op for in-memory stores.
func (s *Store) Reload() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	cfg, err := s.readLocked(s.Get(), isRuntime, false)
	if err != nil {
		return err
	}
	s.Set(cfg)
	return nil
}

// Saved returns the configuration the next start will run with, as far as
// it differs from the running one: the live snapshot with the restart and
// file fields config.yaml holds. Runtime fields stay the live ones (a hand
// edit of one applies on reload, not on restart). Without a file, or when the
// file cannot be read or is invalid, it is the live snapshot.
func (s *Store) Saved() Config {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	live := s.Get()
	saved, err := s.readLocked(live, func(key string) bool { return !isRuntime(key) }, false)
	if err != nil {
		return live
	}
	return saved
}

// PendingRestart returns the restart and file keys whose value in saved (from
// Saved or UpdateAndSave) differs from the running one, in Config order:
// changes that apply when the backend starts again.
func (s *Store) PendingRestart(saved Config) []string {
	return changedKeys(s.Get(), saved, func(key string) bool { return !isRuntime(key) })
}

// readLocked returns base with the fields keep accepts replaced by the
// backing file's; with missingOK, a file that does not exist yields base
// unchanged. The caller holds saveMu.
func (s *Store) readLocked(base Config, keep func(key string) bool, missingOK bool) (Config, error) {
	if s.path == "" {
		return base, nil
	}
	loaded, err := Load(s.path)
	if missingOK && errors.Is(err, fs.ErrNotExist) {
		return base, nil
	}
	if err != nil {
		return Config{}, err
	}
	copyFields(&base, loaded, keep)
	return base, nil
}
