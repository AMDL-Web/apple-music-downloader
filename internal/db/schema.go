package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// migrationStep brings a database from PRAGMA user_version from to to, in
// its own transaction together with the version bump.
type migrationStep struct {
	from, to int
	run      func(ctx context.Context, tx *sql.Tx, env migrationEnv) error
}

// migrationEnv is what a step may need beyond the database itself.
type migrationEnv struct {
	// downloadsDir is download.downloads_dir; stored file paths are made
	// relative to it (storedPath). Empty keeps paths as they are.
	downloadsDir string
	// now stamps rows the migration creates or ends.
	now time.Time
}

// migrations is the schema history as a version map. A shipped step is never
// edited, because databases that already ran it will not run it again; schema
// changes append a new step. Every step preserves the rows of user data (runs,
// their items and milestone events, records and files, the library-sync
// anchor).
//
// Versions 2 to 5 are reserved: the 2.0 release candidates wrote them, and
// their effects are folded into migrateTo6. A database at one of them is
// refused (refuseReleaseCandidate).
var migrations = []migrationStep{
	{0, 1, func(ctx context.Context, tx *sql.Tx, _ migrationEnv) error { return migrateTo1(ctx, tx) }}, // the final 1.x schema, for databases created by any 1.x release
	{1, 6, migrateTo6}, // 2.0: records, files, runs; imports the 1.x jobs
	{6, 7, migrateTo7}, // accounts: per-account library state, record and pending-add storefronts
	{7, 8, migrateTo8}, // runs that name a missing record name none
	{8, 9, migrateTo9}, // rollups of records with a main slot no codec offers
}

// latestSchemaVersion is the user_version a database has after Open.
func latestSchemaVersion() int { return migrations[len(migrations)-1].to }

// isReleaseCandidateSchema reports a user_version only a 2.0 release
// candidate wrote.
func isReleaseCandidateSchema(version int) bool { return version >= 2 && version <= 5 }

// refuseReleaseCandidate is the error Open returns for a database a 2.0
// release candidate wrote. path is the database file, named in full so a
// desktop user can find it.
func refuseReleaseCandidate(path string, version int) error {
	return fmt.Errorf("%s was written by a 2.0 release candidate (schema %d), which this version does not read: move the file aside (it is not changed) and start again", path, version)
}

func schemaVersion(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}) (int, error) {
	var version int
	if err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// migrate applies the steps that lead from the database's user_version to
// target. A database newer than this build, or at a version no step starts
// from, is refused rather than written to.
func migrate(ctx context.Context, database *sql.DB, target int) error {
	return migrateWith(ctx, database, target, migrationEnv{now: now()})
}

func migrateWith(ctx context.Context, database *sql.DB, target int, env migrationEnv) error {
	current, err := schemaVersion(ctx, database)
	if err != nil {
		return err
	}
	if current > latestSchemaVersion() {
		return fmt.Errorf("database schema version %d is newer than this build supports (%d); run a release at least as new as the one that wrote it", current, latestSchemaVersion())
	}
	for current < target {
		step, ok := stepFrom(current)
		if !ok || step.to > target {
			return fmt.Errorf("no migration leads from schema version %d to %d", current, target)
		}
		if err := applyMigration(ctx, database, step, env); err != nil {
			return fmt.Errorf("migrate database to schema version %d: %w", step.to, err)
		}
		current = step.to
	}
	return nil
}

func stepFrom(version int) (migrationStep, bool) {
	for _, step := range migrations {
		if step.from == version {
			return step, true
		}
	}
	return migrationStep{}, false
}

func applyMigration(ctx context.Context, database *sql.DB, step migrationStep, env migrationEnv) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := step.run(ctx, tx, env); err != nil {
		return err
	}
	// PRAGMA takes no bind parameters; step.to is an int from this package.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, step.to)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return tx.Commit()
}

// execAll runs stmts in order inside tx.
func execAll(ctx context.Context, tx *sql.Tx, stmts []string) error {
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func firstLine(stmt string) string {
	for i, r := range stmt {
		if r == '\n' {
			return stmt[:i]
		}
	}
	return stmt
}
