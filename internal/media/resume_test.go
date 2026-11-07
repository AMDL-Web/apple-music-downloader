package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"amdl/internal/config"
	"amdl/internal/domain"
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

func TestCleanupJobArtifactsUsesConfiguredTempDir(t *testing.T) {
	root := t.TempDir()
	cfg := config.Default()
	cfg.Download.TempDir = filepath.Join(root, "temp")
	downloader := newTestDownloader(&Downloader{store: config.NewStore(cfg)})
	alternative := false
	job := domain.Job{ID: "job-clean", Overrides: &config.DownloadOverrides{CodecAlternative: &alternative}}
	path, metadataPath := resumableDownloadPaths(cfg.Download.TempDir, job.ID, "output")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []string{path, metadataPath} {
		if err := os.WriteFile(candidate, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	downloader.CleanupJobArtifacts(job)
	if _, err := os.Stat(resumeOwnerDir(cfg.Download.TempDir, job.ID)); !os.IsNotExist(err) {
		t.Fatalf("job resume directory still exists: %v", err)
	}
}

const testResumeOwner = "job-test"

// An interrupted transfer resumes from its checkpoint with a validated range
// request: If-Range carries the ETag, or the Last-Modified date of a server
// that sends no ETag. The checkpoint's metadata keeps a fingerprint of the
// source, never the signed URL, and is committed complete at the end.
func TestDownloadToFileResumesInterruptedTransfer(t *testing.T) {
	tests := []struct{ name, header, validator string }{
		{"etag", "ETag", `"track-v1"`},
		{"last-modified", "Last-Modified", "Wed, 21 Oct 2015 07:28:00 GMT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := bytes.Repeat([]byte("0123456789abcdef"), 16*1024)
			cut := 73 * 1024
			var mu sync.Mutex
			var requests []http.Header
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.Header.Clone())
				requestNumber := len(requests)
				mu.Unlock()

				w.Header().Set(tt.header, tt.validator)
				if requestNumber == 1 {
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write(payload[:cut])
					return
				}
				if got, want := r.Header.Get("Range"), fmt.Sprintf("bytes=%d-", cut); got != want {
					t.Errorf("Range = %q, want %q", got, want)
				}
				if got := r.Header.Get("If-Range"); got != tt.validator {
					t.Errorf("If-Range = %q, want %q", got, tt.validator)
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(payload)-1, len(payload)))
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)-cut))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = w.Write(payload[cut:])
			}))
			defer server.Close()

			dir := t.TempDir()
			key := filepath.Join(dir, "final.m4a")
			if _, err := downloadToFile(context.Background(), server.Client(), server.URL, dir, testResumeOwner, key, nil); err == nil {
				t.Fatal("first truncated transfer succeeded")
			}
			checkpoint, metadataPath := mustResumePaths(t, dir, testResumeOwner, key)
			if info, err := os.Stat(checkpoint); err != nil || info.Size() != int64(cut) {
				t.Fatalf("partial checkpoint size = %v, err=%v; want %d", info, err, cut)
			}
			metadataBytes, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatalf("resume metadata missing: %v", err)
			}
			if bytes.Contains(metadataBytes, []byte(server.URL)) {
				t.Fatal("resume metadata persisted the signed source URL instead of its fingerprint")
			}

			var progress []float64
			path, err := downloadToFile(context.Background(), server.Client(), server.URL, dir, testResumeOwner, key, func(done, total int64) {
				progress = append(progress, float64(done)/float64(total))
			})
			if err != nil {
				t.Fatal(err)
			}
			assertFileBytes(t, path, payload)
			committedMetadata, err := os.ReadFile(metadataPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(committedMetadata, []byte(`"complete":true`)) {
				t.Fatalf("completed transfer metadata was not committed: %s", committedMetadata)
			}
			if len(progress) == 0 || progress[0] != float64(cut)/float64(len(payload)) || progress[len(progress)-1] != 1 {
				t.Fatalf("resume progress = %v, want first=%v last=1", progress, float64(cut)/float64(len(payload)))
			}
			for i := 1; i < len(progress); i++ {
				if progress[i] < progress[i-1] {
					t.Fatalf("progress decreased at %d: %v", i, progress)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("requests = %d, want 2", len(requests))
			}
			for i, header := range requests {
				if got := header.Get("Accept-Encoding"); got != "identity" {
					t.Errorf("request %d Accept-Encoding = %q, want identity", i+1, got)
				}
			}
		})
	}
}

// rangeReply is one scripted answer of a rangeServer and what the request it
// answers must carry: wantRange and wantIfRange are matched exactly, "" being
// no header at all.
type rangeReply struct {
	wantRange, wantIfRange string
	status                 int // 0 is 200
	header                 map[string]string
	body                   []byte
}

// rangeServer answers its nth request with replies[n] and fails the test on a
// request past the script or one without the headers the reply expects.
func rangeServer(t *testing.T, replies []rangeReply) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if i >= len(replies) {
			t.Errorf("request %d past the script of %d", i+1, len(replies))
			http.Error(w, "unscripted", http.StatusInternalServerError)
			return
		}
		reply := replies[i]
		if got := r.Header.Get("Range"); got != reply.wantRange {
			t.Errorf("request %d Range = %q, want %q", i+1, got, reply.wantRange)
		}
		if got := r.Header.Get("If-Range"); got != reply.wantIfRange {
			t.Errorf("request %d If-Range = %q, want %q", i+1, got, reply.wantIfRange)
		}
		for k, v := range reply.header {
			w.Header().Set(k, v)
		}
		if reply.status != 0 {
			w.WriteHeader(reply.status)
		}
		_, _ = w.Write(reply.body)
	}))
	t.Cleanup(server.Close)
	return server, &n
}

// A download that finds a checkpoint resumes it only when the server's
// answer proves the bytes are the same representation; anything else
// restarts the transfer clean, and the result is always the server's
// current payload.
func TestDownloadToFileValidatesItsCheckpoint(t *testing.T) {
	payload := bytes.Repeat([]byte("current representation"), 2048)
	size := len(payload)
	full := func(etag string) rangeReply {
		h := map[string]string{"Content-Length": strconv.Itoa(size)}
		if etag != "" {
			h["ETag"] = etag
		}
		return rangeReply{header: h, body: payload}
	}
	from := func(n int) string { return fmt.Sprintf("bytes=%d-", n) }
	unsatisfiable := func(etag string, total int) rangeReply {
		h := map[string]string{"Content-Range": fmt.Sprintf("bytes */%d", total)}
		if etag != "" {
			h["ETag"] = etag
		}
		return rangeReply{status: http.StatusRequestedRangeNotSatisfiable, header: h}
	}
	tests := []struct {
		name       string
		checkpoint []byte
		// meta is the checkpoint's metadata; its SourceHash is filled in
		// with the server's fingerprint unless source names another path.
		meta         *resumeMetadata
		source       string
		rawMeta      string
		replies      func(partial int) []rangeReply
		wantProgress []float64
	}{
		{
			name:       "a 200 to the range request replaces the checkpoint",
			checkpoint: []byte("old partial bytes that must not be retained"),
			meta:       &resumeMetadata{ETag: `"v1"`, Total: int64(size)},
			replies: func(partial int) []rangeReply {
				return []rangeReply{{wantRange: from(partial), wantIfRange: `"v1"`, header: map[string]string{"ETag": `"v2"`, "Content-Length": strconv.Itoa(size)}, body: payload}}
			},
		},
		{
			name:       "a 206 under another ETag restarts clean",
			checkpoint: payload[:4096],
			meta:       &resumeMetadata{ETag: `"old"`, Total: int64(size)},
			replies: func(partial int) []rangeReply {
				return []rangeReply{
					{wantRange: from(partial), wantIfRange: `"old"`, status: http.StatusPartialContent, body: payload[partial:],
						header: map[string]string{"ETag": `"changed"`, "Content-Range": fmt.Sprintf("bytes %d-%d/%d", partial, size-1, size)}},
					full(`"changed"`),
				}
			},
		},
		{
			// The header claims 100 bytes while the body is the whole
			// remainder: the 206 is rejected before anything is appended.
			name:       "a 206 whose Content-Range disagrees with its length restarts clean",
			checkpoint: payload[:1024],
			meta:       &resumeMetadata{ETag: `"track"`, Total: int64(size)},
			replies: func(partial int) []rangeReply {
				return []rangeReply{
					{wantRange: from(partial), wantIfRange: `"track"`, status: http.StatusPartialContent, body: payload[partial:],
						header: map[string]string{"ETag": `"track"`, "Content-Range": fmt.Sprintf("bytes %d-%d/%d", partial, partial+99, size), "Content-Length": strconv.Itoa(size - partial)}},
					full(`"track"`),
				}
			},
		},
		{
			// A codec fallback resolves the same output path to another media
			// URI: even with a matching validator the checkpoint is foreign.
			name:       "a checkpoint of another source is not resumed",
			checkpoint: []byte("bytes from the previous variant"),
			meta:       &resumeMetadata{ETag: `"variant"`, Total: int64(size)},
			source:     "/other-variant",
			replies:    func(int) []rangeReply { return []rangeReply{full(`"variant"`)} },
		},
		{
			name:       "a checkpoint without a validator is not resumed",
			checkpoint: []byte("old-prefix"),
			meta:       &resumeMetadata{Total: int64(size)},
			replies:    func(int) []rangeReply { return []rangeReply{full("")} },
		},
		{
			name:       "a full-length checkpoint never marked complete is not trusted",
			checkpoint: bytes.Repeat([]byte("x"), size),
			meta:       &resumeMetadata{ETag: `"same"`, Total: int64(size)},
			replies:    func(int) []rangeReply { return []rangeReply{full(`"same"`)} },
		},
		{
			name:       "corrupt metadata restarts clean",
			checkpoint: []byte("stale"),
			rawMeta:    "{broken",
			replies:    func(int) []rangeReply { return []rangeReply{full("")} },
		},
		{
			name:       "a complete checkpoint the server confirms with 416 is kept",
			checkpoint: payload,
			meta:       &resumeMetadata{ETag: `"complete"`, Total: int64(size), Complete: true},
			replies: func(partial int) []rangeReply {
				r := unsatisfiable(`"complete"`, size)
				r.wantRange, r.wantIfRange = from(partial), `"complete"`
				return []rangeReply{r}
			},
			wantProgress: []float64{1},
		},
		{
			name:       "a complete checkpoint without a known total is kept on 416",
			checkpoint: payload,
			meta:       &resumeMetadata{ETag: `"complete"`, Complete: true},
			replies: func(partial int) []rangeReply {
				r := unsatisfiable("", size)
				r.wantRange, r.wantIfRange = from(partial), `"complete"`
				return []rangeReply{r}
			},
		},
		{
			name:       "a complete checkpoint under a new ETag restarts clean",
			checkpoint: bytes.Repeat([]byte("o"), size),
			meta:       &resumeMetadata{ETag: `"old"`, Total: int64(size), Complete: true},
			replies: func(partial int) []rangeReply {
				r := unsatisfiable(`"new"`, size)
				r.wantRange, r.wantIfRange = from(partial), `"old"`
				return []rangeReply{r, full(`"new"`)}
			},
		},
		{
			name:       "a 416 whose total is not the checkpoint's restarts clean",
			checkpoint: bytes.Repeat([]byte("s"), size+7),
			meta:       &resumeMetadata{ETag: `"old"`},
			replies: func(partial int) []rangeReply {
				r := unsatisfiable("", size)
				r.wantRange, r.wantIfRange = from(partial), `"old"`
				return []rangeReply{r, full("")}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			key := "output"
			checkpoint, metadataPath := mustResumePaths(t, dir, testResumeOwner, key)
			if err := os.WriteFile(checkpoint, tt.checkpoint, 0o600); err != nil {
				t.Fatal(err)
			}
			replies := tt.replies(len(tt.checkpoint))
			server, requests := rangeServer(t, replies)
			switch {
			case tt.rawMeta != "":
				if err := os.WriteFile(metadataPath, []byte(tt.rawMeta), 0o600); err != nil {
					t.Fatal(err)
				}
			case tt.meta != nil:
				meta := *tt.meta
				meta.Version = resumeMetadataVersion
				meta.SourceHash = sourceFingerprint(server.URL + tt.source)
				meta.Synced = int64(len(tt.checkpoint))
				if err := writeResumeMetadata(metadataPath, meta); err != nil {
					t.Fatal(err)
				}
			}

			var progress []float64
			path, err := downloadToFile(context.Background(), server.Client(), server.URL, dir, testResumeOwner, key, func(done, total int64) {
				progress = append(progress, float64(done)/float64(total))
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := int(requests.Load()); got != len(replies) {
				t.Fatalf("requests = %d, want the %d scripted", got, len(replies))
			}
			assertFileBytes(t, path, payload)
			if tt.wantProgress != nil && !slices.Equal(progress, tt.wantProgress) {
				t.Fatalf("progress = %v, want %v", progress, tt.wantProgress)
			}
		})
	}
}

func TestSourceFingerprintIgnoresQuerySignatures(t *testing.T) {
	base := "https://cdn.example.com/item/track.mp4"
	if sourceFingerprint(base+"?sig=abc&expires=1") != sourceFingerprint(base+"?sig=def&expires=2") {
		t.Fatal("rotated query signatures must map to the same fingerprint")
	}
	if sourceFingerprint(base) == sourceFingerprint("https://cdn.example.com/item/other.mp4") {
		t.Fatal("different paths must map to different fingerprints")
	}
}

func TestCleanupResumableDownloadRemovesMediaAndMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resume-test.mp4")
	for _, candidate := range []string{path, path + ".json", path + ".json.tmp"} {
		if err := os.WriteFile(candidate, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cleanupResumableDownload(path)
	for _, candidate := range []string{path, path + ".json", path + ".json.tmp"} {
		if _, err := os.Stat(candidate); !os.IsNotExist(err) {
			t.Errorf("checkpoint artifact %s still exists (err=%v)", candidate, err)
		}
	}
}

func TestCleanupResumeOwnerRemovesAllCodecArtifacts(t *testing.T) {
	dir := t.TempDir()
	owner := "job-with-fallback"
	for _, key := range []string{"alac-output", "aac-output"} {
		path, metadataPath := resumableDownloadPaths(dir, owner, key)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(metadataPath, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerDir := resumeOwnerDir(dir, owner)
	cleanupResumeOwner(dir, owner)
	if _, err := os.Stat(ownerDir); !os.IsNotExist(err) {
		t.Fatalf("owner checkpoint directory still exists: %v", err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file content differs: got %d bytes, want %d", len(got), len(want))
	}
}

func mustResumePaths(t *testing.T, dir, owner, key string) (string, string) {
	t.Helper()
	path, metadataPath := resumableDownloadPaths(dir, owner, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, metadataPath
}
