package db

import (
	"testing"
	"time"
)

func TestFormatTimeFixedWidth(t *testing.T) {
	exact := time.Date(2024, 7, 10, 12, 0, 0, 0, time.UTC)
	frac := time.Date(2024, 7, 10, 12, 0, 0, 123000000, time.UTC)
	if got, want := formatTime(exact), "2024-07-10T12:00:00.000000000Z"; got != want {
		t.Fatalf("formatTime(exact) = %q, want %q", got, want)
	}
	if got, want := formatTime(frac), "2024-07-10T12:00:00.123000000Z"; got != want {
		t.Fatalf("formatTime(frac) = %q, want %q", got, want)
	}
	// Lexicographic order must match chronological order across the boundary.
	if !(formatTime(exact) < formatTime(frac) && formatTime(frac) < formatTime(exact.Add(time.Second))) {
		t.Fatalf("fixed-width strings are not chronologically ordered: %q %q %q",
			formatTime(exact), formatTime(frac), formatTime(exact.Add(time.Second)))
	}
}
