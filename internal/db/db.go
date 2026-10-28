package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"

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

// Store is the SQLite-backed persistence for jobs, items, events and the
// library-sync anchor. It is safe for concurrent use; the pool has one
// connection, so statements run one at a time.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies every
// pending schema migration (see migrations) before returning. Nothing
// schema-shaped runs after that.
func Open(path string) (*Store, error) {
	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
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
	if err := migrate(context.Background(), database, latestSchemaVersion()); err != nil {
		_ = database.Close()
		return nil, err
	}
	// Per-job overrides may contain a media-user-token while a station/private
	// playlist is queued or retryable. Keep the database and its live WAL files
	// owner-only, matching the managed config file that can hold the global
	// fallback token. SQLite derives future sidecar modes from the main file.
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		for _, databaseFile := range []string{path, path + "-wal", path + "-shm"} {
			if err := os.Chmod(databaseFile, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
				_ = database.Close()
				return nil, fmt.Errorf("restrict database permissions for %s: %w", databaseFile, err)
			}
		}
	}
	return &Store{db: database}, nil
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
