package db

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// schema1x is the frozen 1.x schema and rows1x one of each kind of row a
// 1.x install carries; see the comments in the files.
var (
	//go:embed testdata/schema_1x.sql
	schema1x string
	//go:embed testdata/rows_1x.sql
	rows1x string
)

func create1xDatabase(t *testing.T) string {
	t.Helper()
	return createRawDatabase(t, schema1x, rows1x)
}

type tableDump map[string]map[string]any // primary key -> column -> value

// dumpTable reads every row of table, keyed by the SQL expression key.
func dumpTable(t *testing.T, database *sql.DB, table, key string) tableDump {
	t.Helper()
	rows, err := database.Query(`SELECT *, (` + key + `) AS dump_key FROM ` + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	out := tableDump{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := map[string]any{}
		for i, col := range cols {
			row[col] = values[i]
		}
		dumpKey := fmt.Sprint(row["dump_key"])
		delete(row, "dump_key")
		out[dumpKey] = row
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// dump1x dumps the user data of a 1.x schema.
func dump1x(t *testing.T, database *sql.DB) map[string]tableDump {
	t.Helper()
	return map[string]tableDump{
		"jobs":                dumpTable(t, database, "jobs", "id"),
		"job_items":           dumpTable(t, database, "job_items", "id"),
		"job_events":          dumpTable(t, database, "job_events", "id"),
		"library_sync_anchor": dumpTable(t, database, "library_sync_anchor", "position"),
	}
}

// dumpAll dumps every table of the 2.0 schema that holds user data, plus
// the AUTOINCREMENT high-water marks.
func dumpAll(t *testing.T, database *sql.DB) map[string]tableDump {
	t.Helper()
	return map[string]tableDump{
		"runs":                dumpTable(t, database, "runs", "id"),
		"run_items":           dumpTable(t, database, "run_items", "id"),
		"run_events":          dumpTable(t, database, "run_events", "id"),
		"albums":              dumpTable(t, database, "albums", "id"),
		"tracks":              dumpTable(t, database, "tracks", "album_id || '/' || song_id"),
		"playlists":           dumpTable(t, database, "playlists", "id"),
		"playlist_tracks":     dumpTable(t, database, "playlist_tracks", "playlist_id || '/' || song_id"),
		"files":               dumpTable(t, database, "files", "owner_kind || '/' || owner_id || '/' || song_id || '/' || slot"),
		"pending_adds":        dumpTable(t, database, "pending_adds", "kind || '/' || catalog_id"),
		"resource_sources":    dumpTable(t, database, "resource_sources", "resource_kind || '/' || resource_id || '/' || source"),
		"deleted_resources":   dumpTable(t, database, "deleted_resources", "resource_kind || '/' || resource_id"),
		"library_sync_anchor": dumpTable(t, database, "library_sync_anchor", "position"),
		"sqlite_sequence":     dumpTable(t, database, "sqlite_sequence", "name"),
	}
}

func isTimestampColumn(col string) bool {
	return strings.HasSuffix(col, "_at")
}

// assertRowsSurvived checks that every row of before is still in after with
// the same value in every column after still has. Timestamps may have been
// rewritten to the fixed-width form, so they compare as instants.
func assertRowsSurvived(t *testing.T, table string, before, after tableDump, skip func(key, col string) bool) {
	t.Helper()
	for key, oldRow := range before {
		newRow, ok := after[key]
		if !ok {
			t.Errorf("%s row %s was lost", table, key)
			continue
		}
		for col, oldValue := range oldRow {
			newValue, kept := newRow[col]
			if !kept || (skip != nil && skip(key, col)) {
				continue
			}
			if isTimestampColumn(col) {
				oldTime, newTime := parseTime(fmt.Sprint(oldValue)), parseTime(fmt.Sprint(newValue))
				if oldTime.IsZero() || !oldTime.Equal(newTime) {
					t.Errorf("%s %s.%s = %v, want the instant %v", table, key, col, newValue, oldValue)
				}
				continue
			}
			if !reflect.DeepEqual(oldValue, newValue) {
				t.Errorf("%s %s.%s = %#v, want %#v", table, key, col, newValue, oldValue)
			}
		}
	}
}

func columnsOf(t *testing.T, database *sql.DB, table string) map[string]bool {
	t.Helper()
	rows, err := database.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out[name] = true
	}
	return out
}

func indexExists(t *testing.T, database *sql.DB, name string) bool {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count == 1
}

func userVersion(t *testing.T, database *sql.DB) int {
	t.Helper()
	version, err := schemaVersion(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
