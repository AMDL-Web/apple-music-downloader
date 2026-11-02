package media

import (
	"os"
	"path/filepath"
	"sync"

	"amdl/internal/domain"
)

// statDiskOutput reports a regular file at path and its size.
func statDiskOutput(path string) (int64, bool) {
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return 0, false
	}
	return fi.Size(), true
}

// outputSet is what simulate mode has committed: the paths a simulated track
// "wrote", with the size it reported. NewDownloader creates one and every
// withConfig clone shares it, like the media pools, so a later job sees what
// an earlier one finished. It lives in memory only and is empty after a
// restart. A nil set is empty and records nothing.
type outputSet struct {
	mu    sync.Mutex
	sizes map[string]int64
}

func newOutputSet() *outputSet { return &outputSet{sizes: make(map[string]int64)} }

func outputKey(path string) string { return filepath.Clean(domain.AbsolutePath(path)) }

func (s *outputSet) add(path string, size int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sizes[outputKey(path)] = size
}

func (s *outputSet) stat(path string) (int64, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	size, ok := s.sizes[outputKey(path)]
	return size, ok
}
