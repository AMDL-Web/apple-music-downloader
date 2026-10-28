package db

import (
	"database/sql"
	"time"
)

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// timeLayoutFixed is a fixed-width UTC RFC3339 with exactly 9 fractional
// digits. Unlike time.RFC3339Nano (which trims trailing zeros / omits the
// fraction entirely for whole seconds), this keeps lexicographic TEXT order
// identical to chronological order in SQLite comparisons, MIN/MAX and ORDER BY.
const timeLayoutFixed = "2006-01-02T15:04:05.000000000Z07:00"

// fixedTimestampGLOB matches formatTime output in SQLite GLOB syntax
// (? = one character).
const fixedTimestampGLOB = "????-??-??T??:??:??.?????????Z"

func formatTime(t time.Time) string { return t.UTC().Format(timeLayoutFixed) }

// formatOptionalTime maps a nil time to SQL NULL.
func formatOptionalTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseTime(v string) time.Time {
	// Migration 1 rewrote legacy variable-width rows, but parsing stays
	// tolerant of them so a hand-edited row cannot zero a timestamp.
	if t, err := time.Parse(timeLayoutFixed, v); err == nil {
		return t
	}
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}

// parseOptionalTime maps SQL NULL (and an empty string) to nil.
func parseOptionalTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parseTime(v.String)
	return &t
}
