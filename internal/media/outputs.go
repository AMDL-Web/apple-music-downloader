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
// "wrote", with the size it reported and its codec. NewDownloader creates one
// and every withConfig clone shares it, like the media pools, so a later job
// sees what an earlier one finished. It lives in memory only and is empty
// after a restart. A nil set is empty and records nothing.
type outputSet struct {
	mu    sync.Mutex
	files map[string]simulatedOutput
}

// simulatedOutput is one simulated commit.
type simulatedOutput struct {
	size  int64
	codec string
}

func newOutputSet() *outputSet { return &outputSet{files: make(map[string]simulatedOutput)} }

func outputKey(path string) string { return filepath.Clean(domain.AbsolutePath(path)) }

func (s *outputSet) add(path string, size int64, codec string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[outputKey(path)] = simulatedOutput{size: size, codec: codec}
}

func (s *outputSet) stat(path string) (int64, bool) {
	f, ok := s.lookup(path)
	return f.size, ok
}

func (s *outputSet) lookup(path string) (simulatedOutput, bool) {
	if s == nil {
		return simulatedOutput{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[outputKey(path)]
	return f, ok
}
