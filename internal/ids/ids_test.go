package ids

import (
	"regexp"
	"testing"
)

func TestNewIDFormat(t *testing.T) {
	pattern := regexp.MustCompile(`^(job|item)_[0-9a-f]{32}$`)
	for _, prefix := range []string{"job", "item"} {
		id := NewID(prefix)
		if !pattern.MatchString(id) || id[:len(prefix)+1] != prefix+"_" {
			t.Fatalf("NewID(%q) = %q, want %s_ followed by 32 lowercase hex digits", prefix, id, prefix)
		}
	}
}

func TestNewIDUnique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewID("job")
		if _, dup := seen[id]; dup {
			t.Fatalf("NewID returned %q twice within %d calls", id, i+1)
		}
		seen[id] = struct{}{}
	}
}
