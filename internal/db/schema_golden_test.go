package db

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/schema.golden from a fresh database")

// dumpSchema renders the schema of database as text that does not depend on
// how it was reached: tables with their columns (in order), foreign keys and
// AUTOINCREMENT, indexes with their columns, uniqueness and partial WHERE,
// and triggers and views as their whitespace-normalized SQL. ALTER TABLE
// rewrites a table's CREATE text, so table SQL itself is not compared.
func dumpSchema(t *testing.T, database *sql.DB) string {
	t.Helper()
	var out strings.Builder
	type object struct{ kind, name, table, sql string }
	var objects []object
	rows, err := database.Query(`SELECT type, name, tbl_name, COALESCE(sql, '') FROM sqlite_master
		WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.kind, &o.name, &o.table, &o.sql); err != nil {
			t.Fatal(err)
		}
		objects = append(objects, o)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	query := func(q string, args ...any) [][]string {
		t.Helper()
		rows, err := database.Query(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var res [][]string
		for rows.Next() {
			values := make([]sql.NullString, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			row := make([]string, len(cols))
			for i, v := range values {
				row[i] = v.String
				if !v.Valid {
					row[i] = "NULL"
				}
			}
			res = append(res, row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return res
	}
	space := regexp.MustCompile(`\s+`)
	normalize := func(s string) string {
		s = space.ReplaceAllString(strings.TrimSpace(s), " ")
		s = strings.ReplaceAll(s, " IF NOT EXISTS", "")
		s = strings.ReplaceAll(s, "( ", "(")
		return strings.ReplaceAll(s, " )", ")")
	}
	for _, o := range objects {
		switch o.kind {
		case "table":
			fmt.Fprintf(&out, "table %s", o.name)
			if strings.Contains(strings.ToUpper(o.sql), "AUTOINCREMENT") {
				out.WriteString(" autoincrement")
			}
			out.WriteString("\n")
			for _, c := range query(`SELECT name, type, "notnull", dflt_value, pk FROM pragma_table_xinfo(?) ORDER BY cid`, o.name) {
				fmt.Fprintf(&out, "  column %s %s notnull=%s default=%s pk=%s\n", c[0], c[1], c[2], c[3], c[4])
			}
			// UNIQUE and PRIMARY KEY constraints get automatic indexes,
			// whose names depend on declaration order: list them by columns.
			for _, ix := range query(`SELECT name, origin FROM pragma_index_list(?) WHERE origin <> 'c' ORDER BY name`, o.name) {
				var cols []string
				for _, c := range query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, ix[0]) {
					cols = append(cols, c[0])
				}
				fmt.Fprintf(&out, "  constraint %s(%s)\n", ix[1], strings.Join(cols, ", "))
			}
			for _, fk := range query(`SELECT "from", "table", "to", on_update, on_delete FROM pragma_foreign_key_list(?) ORDER BY id, seq`, o.name) {
				fmt.Fprintf(&out, "  foreign key %s -> %s(%s) on update %s on delete %s\n", fk[0], fk[1], fk[2], fk[3], fk[4])
			}
		case "index":
			info := query(`SELECT "unique", origin, partial FROM pragma_index_list(?) WHERE name = ?`, o.table, o.name)
			if len(info) != 1 {
				t.Fatalf("index %s not listed on %s", o.name, o.table)
			}
			var cols []string
			for _, c := range query(`SELECT COALESCE(name, '<expr>'), "desc", coll FROM pragma_index_xinfo(?) WHERE key = 1 ORDER BY seqno`, o.name) {
				col := c[0]
				if c[1] == "1" {
					col += " DESC"
				}
				if c[2] != "BINARY" {
					col += " COLLATE " + c[2]
				}
				cols = append(cols, col)
			}
			fmt.Fprintf(&out, "index %s on %s(%s) unique=%s origin=%s", o.name, o.table, strings.Join(cols, ", "), info[0][0], info[0][1])
			if info[0][2] == "1" {
				where := normalize(o.sql)
				where = where[strings.LastIndex(strings.ToUpper(where), " WHERE ")+1:]
				fmt.Fprintf(&out, " %s", where)
			}
			out.WriteString("\n")
		default:
			fmt.Fprintf(&out, "%s %s: %s\n", o.kind, o.name, normalize(o.sql))
		}
	}
	return out.String()
}

func schemaOf(t *testing.T, path string, opts Options) string {
	t.Helper()
	store, err := OpenWith(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	return dumpSchema(t, store.db)
}

// A database that reached the latest version through every migration has
// exactly the schema a fresh one gets: no migration may forget an index,
// column, default or constraint that only fresh databases have (or the
// reverse). The fresh schema is pinned in testdata/schema.golden so a schema
// change shows up in review; run go test -run TestSchema -update to accept one.
func TestSchemaOfMigratedDatabasesEqualsFresh(t *testing.T) {
	fresh := schemaOf(t, t.TempDir()+"/amdl.db", Options{})

	const golden = "testdata/schema.golden"
	if *update {
		if err := os.WriteFile(golden, []byte(fresh), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test -run TestSchema -update to create it)", err)
	}
	if fresh != string(want) {
		t.Errorf("fresh schema differs from %s (run go test -run TestSchema -update if the change is intended):\n%s", golden, lineDiff(string(want), fresh))
	}

	for name, path := range map[string]string{
		"1.x":                        create1xDatabase(t),
		"pre-artwork columns":        createRawDatabase(t, legacySQL(t, "legacy_no_artwork.sql")),
		"variable-width timestamps":  createRawDatabase(t, legacySQL(t, "legacy_variable_timestamps.sql")),
		"single progress job_items":  createRawDatabase(t, legacySQL(t, "legacy_item_progress.sql")),
		"schema 6 with unowned rows": create6Database(t),
	} {
		if got := schemaOf(t, path, Options{DownloadsDir: "/music"}); got != fresh {
			t.Errorf("schema of the migrated %s database differs from a fresh one:\n%s", name, lineDiff(fresh, got))
		}
	}
}

// lineDiff lists the lines only want has (-) and only got has (+).
func lineDiff(want, got string) string {
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, line := range strings.Split(s, "\n") {
			m[line]++
		}
		return m
	}
	w, g := count(want), count(got)
	var out strings.Builder
	for _, line := range strings.Split(want, "\n") {
		if g[line] < w[line] {
			fmt.Fprintf(&out, "- %s\n", line)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if w[line] < g[line] {
			fmt.Fprintf(&out, "+ %s\n", line)
		}
	}
	if out.Len() == 0 {
		return "(same lines, different order)"
	}
	return out.String()
}
