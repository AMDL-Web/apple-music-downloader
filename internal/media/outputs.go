package media

import (
	"os"
	"path/filepath"
	"strings"
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

// diskOutputNames lists the finished tracks directly in dir: regular .m4a
// files whose name does not start with a dot. Staging files end in .part and
// sidecars and covers have their own extensions, so none of them count.
func diskOutputNames(dir string) map[string]struct{} {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(strings.ToLower(name), ".m4a") {
			continue
		}
		// A symlinked track counts as statOutput would count it: by its target.
		if entry.Type()&os.ModeSymlink != 0 {
			if _, ok := statDiskOutput(filepath.Join(dir, name)); !ok {
				continue
			}
		} else if !entry.Type().IsRegular() {
			continue
		}
		names[name] = struct{}{}
	}
	return names
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

// names lists the base names of the committed paths directly in dir.
func (s *outputSet) names(dir string) []string {
	if s == nil {
		return nil
	}
	dir = outputKey(dir)
	s.mu.Lock()
	defer s.mu.Unlock()
	var names []string
	for path := range s.sizes {
		if filepath.Dir(path) == dir {
			names = append(names, filepath.Base(path))
		}
	}
	return names
}
