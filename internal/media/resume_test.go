package media

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// A track that finds no checkpoint of its own leaves the job's checkpoint
// directory alone: a sibling track may just have created it and be about to
// open its checkpoint there.
func TestMissingCheckpointKeepsTheJobsDirectory(t *testing.T) {
	dir := t.TempDir()
	sibling, _ := resumableDownloadPaths(dir, testResumeOwner, "sibling.m4a")
	if err := os.MkdirAll(filepath.Dir(sibling), 0o700); err != nil {
		t.Fatal(err)
	}
	path, metadataPath := resumableDownloadPaths(dir, testResumeOwner, "track.m4a")
	if offset, _ := loadResumeCheckpoint(path, metadataPath, "https://example.com/track.mp4"); offset != 0 {
		t.Fatalf("offset = %d without a checkpoint, want 0", offset)
	}
	if _, err := os.Stat(filepath.Dir(sibling)); err != nil {
		t.Fatalf("the job's checkpoint directory is gone: %v", err)
	}
}

// Tracks of one job open checkpoints in the job's directory while others look
// for theirs or clean up after decrypting, and every open succeeds.
func TestConcurrentCheckpointsShareTheJobsDirectory(t *testing.T) {
	dir := t.TempDir()
	const rounds = 2000
	var wg sync.WaitGroup
	errs := make(chan error, rounds)
	wg.Go(func() {
		for i := range rounds {
			path, _ := resumableDownloadPaths(dir, testResumeOwner, "opener.m4a")
			sink := &fileSink{path: path}
			if err := sink.open(); err != nil {
				errs <- err
				continue
			}
			_ = sink.close()
			if i%2 == 0 {
				cleanupResumableDownload(path)
			}
		}
	})
	wg.Go(func() {
		for range rounds {
			path, metadataPath := resumableDownloadPaths(dir, testResumeOwner, "prober.m4a")
			loadResumeCheckpoint(path, metadataPath, "https://example.com/prober.mp4")
		}
	})
	wg.Wait()
	close(errs)
	failed := 0
	var first error
	for err := range errs {
		if first == nil {
			first = err
		}
		if errors.Is(err, fs.ErrNotExist) {
			failed++
		}
	}
	if first != nil {
		t.Fatalf("%d of %d checkpoint opens failed, first: %v", failed, rounds, first)
	}
}

// writeTestCheckpoint leaves a checkpoint of size bytes whose metadata says
// synced of them were fsynced, as a crash would leave a partly written one.
func writeTestCheckpoint(t *testing.T, size, synced int64) (string, string, string) {
	t.Helper()
	uri := "https://example.com/track.mp4"
	path, metadataPath := mustResumePaths(t, t.TempDir(), testResumeOwner, "track.m4a")
	if err := os.WriteFile(path, bytes.Repeat([]byte{7}, int(size)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeResumeMetadata(metadataPath, resumeMetadata{
		Version: resumeMetadataVersion, SourceHash: sourceFingerprint(uri), ETag: `"v1"`, Total: 4 * size, Synced: synced,
	}); err != nil {
		t.Fatal(err)
	}
	return path, metadataPath, uri
}

// Bytes past the synced length may be pages a power loss never wrote: the
// resume starts at the synced length and the file is cut back to it.
func TestCheckpointResumesFromItsSyncedBytesOnly(t *testing.T) {
	path, metadataPath, uri := writeTestCheckpoint(t, 1000, 600)
	offset, _ := loadResumeCheckpoint(path, metadataPath, uri)
	if offset != 600 {
		t.Fatalf("offset = %d, want the 600 synced bytes", offset)
	}
	if info, err := os.Stat(path); err != nil || info.Size() != 600 {
		t.Fatalf("checkpoint after load: %v, err %v; want it cut back to 600 bytes", info, err)
	}
}

// A checkpoint with nothing synced holds no byte a resume may trust.
func TestCheckpointWithNothingSyncedRestarts(t *testing.T) {
	path, metadataPath, uri := writeTestCheckpoint(t, 1000, 0)
	if offset, _ := loadResumeCheckpoint(path, metadataPath, uri); offset != 0 {
		t.Fatalf("offset = %d, want a restart", offset)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("checkpoint still present after a restart: %v", err)
	}
}

// The sink fsyncs once enough bytes have piled up and again on close, and
// reports the length after each sync.
func TestFileSinkReportsEachSync(t *testing.T) {
	path, _ := mustResumePaths(t, t.TempDir(), testResumeOwner, "track.m4a")
	var reported []int64
	sink := &fileSink{path: path, syncEvery: 1024, onSynced: func(n int64) { reported = append(reported, n) }}
	for range 3 {
		if _, err := sink.Write(make([]byte, 1000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.close(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(reported, []int64{2000, 3000}) {
		t.Fatalf("synced lengths %v, want 2000 (past 1024 unsynced) and 3000 (close)", reported)
	}
}
