package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrDuplicateActive is returned by CreateJob when the job's canonical_key
// collides with another queued/running job (partial unique index backstop).
var ErrDuplicateActive = errors.New("duplicate active job")

// ErrJobNotFound is returned by DeleteJob when no job has the given id.
var ErrJobNotFound = errors.New("job not found")

// ErrJobNotTerminal is returned by DeleteJob when the job is still queued or
// running; active jobs must be cancelled before they can be deleted.
var ErrJobNotTerminal = errors.New("job is not in a terminal status")

func isUniqueConstraintErr(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE || sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT
	}
	return false
}

// Store is the SQLite-backed persistence for records, their files, runs,
// items, events, pending adds and the library-sync anchor. It is safe for
// concurrent use; the pool has one connection, so statements run one at a
// time.
type Store struct {
	db           *sql.DB
	downloadsDir atomic.Pointer[string]
	// downloadsDirFn, when set, is the live downloads dir (FollowDownloadsDir).
	downloadsDirFn func() string
}

// Options configure OpenWith.
type Options struct {
	// DownloadsDir is download.downloads_dir. File paths under it are stored
	// relative to it (domain.File.Path); empty stores paths as given.
	DownloadsDir string
}

// Open is OpenWith without options.
func Open(path string) (*Store, error) { return OpenWith(path, Options{}) }

// OpenWith opens (creating if needed) the database at path and applies every
// pending schema migration (see migrations) before returning. Nothing
// schema-shaped runs after that. A database a 2.0 release candidate wrote is
// refused before anything is written to it.
func OpenWith(path string, opts Options) (*Store, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	version, err := schemaVersion(context.Background(), database)
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if isReleaseCandidateSchema(version) {
		_ = database.Close()
		return nil, refuseReleaseCandidate(filepath.Base(path), version)
	}
	// WAL avoids a full journal+fsync cycle per statement; NORMAL still syncs
	// at WAL checkpoints, which is durable enough for resumable download state.
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
	} {
		if _, err := database.Exec(pragma); err != nil {
			_ = database.Close()
			return nil, fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	if err := migrateWith(context.Background(), database, latestSchemaVersion(), migrationEnv{downloadsDir: opts.DownloadsDir, now: now()}); err != nil {
		_ = database.Close()
		return nil, err
	}
	// Job rows written before overrides lost media_user_token may still hold
	// one. Keep the database and its live WAL files owner-only, matching the
	// managed config file that holds the token. SQLite derives future sidecar modes from the main file.
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		for _, databaseFile := range []string{path, path + "-wal", path + "-shm"} {
			if err := os.Chmod(databaseFile, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
				_ = database.Close()
				return nil, fmt.Errorf("restrict database permissions for %s: %w", databaseFile, err)
			}
		}
	}
	store := &Store{db: database}
	store.SetDownloadsDir(opts.DownloadsDir)
	return store, nil
}

// SetDownloadsDir changes the directory file paths are stored relative to,
// for writes from now on. Stored paths are not rewritten.
func (s *Store) SetDownloadsDir(dir string) { s.downloadsDir.Store(&dir) }

// FollowDownloadsDir makes the store read the downloads dir from get on every
// use, so a runtime change of download.downloads_dir applies to the next
// write and read. Call it before the store is shared.
func (s *Store) FollowDownloadsDir(get func() string) { s.downloadsDirFn = get }

// storedPath is p as files.path stores it under the current downloads dir.
func (s *Store) storedPath(p string) string { return storedPath(s.DownloadsDir(), p) }

// DownloadsDir returns the directory file paths are stored relative to.
func (s *Store) DownloadsDir() string {
	if s.downloadsDirFn != nil {
		return s.downloadsDirFn()
	}
	return *s.downloadsDir.Load()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// execer is satisfied by both *sql.DB and *sql.Tx, letting the same query
// helper run standalone or as part of a larger transaction.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
