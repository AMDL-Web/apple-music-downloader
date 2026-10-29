package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// The integrity check's error is what an operator reads on a failed item, so it
// must carry ffmpeg's own diagnosis.
func TestCheckIntegrityFileReportsFFmpegDiagnostics(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.m4a")
	if err := os.WriteFile(file, []byte("not audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "decoder errors and failing exit",
			script: `echo "[aac @ 0x1] channel element 0.0 is not allocated" >&2; exit 1`,
			want:   []string{"ffmpeg decode: exit status 1", "channel element 0.0 is not allocated"},
		},
		{
			name:   "decoder errors with a clean exit",
			script: `echo "[alac @ 0x1] invalid samples per frame" >&2`,
			want:   []string{"ffmpeg decode reported errors", "invalid samples per frame"},
		},
		{
			name:   "failing exit without output",
			script: `exit 3`,
			want:   []string{"ffmpeg decode: exit status 3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPipeTestProcessor(t, writePipeTestCommand(t, tt.script))
			err := p.checkIntegrityFile(context.Background(), file)
			if err == nil {
				t.Fatal("check passed, want an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}

	t.Run("clean decode", func(t *testing.T) {
		p := newPipeTestProcessor(t, writePipeTestCommand(t, "exit 0"))
		if err := p.checkIntegrityFile(context.Background(), file); err != nil {
			t.Fatalf("clean check = %v, want nil", err)
		}
	})

	t.Run("long output is capped", func(t *testing.T) {
		// 200 lines of 41 bytes is 8200 bytes of diagnostics.
		p := newPipeTestProcessor(t, writePipeTestCommand(t,
			`i=0; while [ $i -lt 200 ]; do echo "[aac @ 0x1] error decoding packet $i" >&2; i=$((i+1)); done; exit 1`))
		err := p.checkIntegrityFile(context.Background(), file)
		if err == nil {
			t.Fatal("check passed, want an error")
		}
		if len(err.Error()) > maxToolDiagnosticBytes+200 {
			t.Fatalf("error is %d bytes, want it capped near %d", len(err.Error()), maxToolDiagnosticBytes)
		}
		if !strings.Contains(err.Error(), "error decoding packet 0") || !strings.Contains(err.Error(), "more bytes)") {
			t.Fatalf("error = %q, want the first lines and a truncation note", err)
		}
	})
}

func TestWriteFileAtomicLeavesOnlyTheFinishedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cover.jpg")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new cover")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new cover" {
		t.Fatalf("cover = %q (err %v), want the new bytes", got, err)
	}
	assertMode0644(t, path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the cover", len(entries))
	}

	// A write that cannot be renamed into place (the target is a directory)
	// fails and leaves no temp file behind.
	blocked := filepath.Join(dir, "artist.jpg")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(blocked, []byte("x")); err == nil {
		t.Fatal("writing over a directory succeeded")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("directory holds %v after a failed write, want no leftover temp file", entries)
	}
}

func TestLyricsSidecarsAreWrittenNextToTheTrack(t *testing.T) {
	cfg := config.Default()
	cfg.Download.LyricsFiles = []string{"lrc", "ttml"}
	cfg.Download.CheckIntegrity = false
	d := newTestDownloader(&Downloader{cfg: cfg})
	staged := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(staged, []byte("tagged audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "Album", "01. Song.m4a")
	err := d.finalizeTrackOutput(context.Background(), sidecarOnlyMedia{realMedia{d: d}}, trackOutput{
		item: &domain.JobItem{ID: "item-1"}, song: applemusic.Song{Name: "Song"}, codec: "aac-lc",
		lyrics: trackLyrics{lrc: "[00:01.00]line", ttml: "<tt/>"}, staged: stagedFile{path: staged}, outPath: out, labels: aacLCOutputLabels,
	}, noopStage)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "01. Song.ttml")); err != nil || string(got) != "<tt/>" {
		t.Fatalf("ttml sidecar = %q (err %v), want the TTML next to the track", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "01. Song.lrc")); err != nil || string(got) != "[00:01.00]line" {
		t.Fatalf("lrc sidecar = %q (err %v), want the LRC next to the track", got, err)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != "tagged audio" {
		t.Fatalf("track = %q (err %v), want the committed staged file", got, err)
	}
}

// With embed_lyrics off the tag gets no lyrics, even while a sidecar is written.
func TestEmbedLyricsOffKeepsLyricsOutOfTheTag(t *testing.T) {
	cfg := config.Default()
	cfg.Download.EmbedLyrics = false
	cfg.Download.LyricsFiles = []string{"lrc"}
	cfg.Download.CheckIntegrity = false
	d := newTestDownloader(&Downloader{cfg: cfg})
	staged := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(staged, []byte("tagged audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "Album", "01. Song.m4a")
	media := &tagRecordingMedia{realMedia: realMedia{d: d}}
	err := d.finalizeTrackOutput(context.Background(), media, trackOutput{
		item: &domain.JobItem{ID: "item-1"}, song: applemusic.Song{Name: "Song"}, codec: "aac-lc",
		lyrics: trackLyrics{lrc: "[00:01.00]line", ttml: "<tt/>"}, staged: stagedFile{path: staged}, outPath: out, labels: aacLCOutputLabels,
	}, noopStage)
	if err != nil {
		t.Fatal(err)
	}
	if !media.tagged || media.taggedLyrics != "" {
		t.Fatalf("tag called = %v with lyrics %q, want a tag without lyrics", media.tagged, media.taggedLyrics)
	}
	if got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "01. Song.lrc")); err != nil || string(got) != "[00:01.00]line" {
		t.Fatalf("lrc sidecar = %q (err %v), want it written", got, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(out), "01. Song.ttml")); !os.IsNotExist(err) {
		t.Fatalf("ttml sidecar stat err = %v, want none when lyrics_files lacks ttml", err)
	}
}

// sidecarOnlyMedia skips tagging, which needs a real MP4.
type sidecarOnlyMedia struct{ realMedia }

// tagRecordingMedia records what tagging was asked to embed instead of
// tagging, which needs a real MP4.
type tagRecordingMedia struct {
	realMedia
	tagged       bool
	taggedLyrics string
}

func (m *tagRecordingMedia) tag(_ context.Context, _ stagedFile, _ applemusic.Song, lyrics string, _ []byte) error {
	m.tagged, m.taggedLyrics = true, lyrics
	return nil
}

func (sidecarOnlyMedia) tag(context.Context, stagedFile, applemusic.Song, string, []byte) error {
	return nil
}

func noopStage(domain.ItemStatus, string, func(*domain.ItemProgress)) {}

func TestDownloaderWaitDrainsBackgroundWork(t *testing.T) {
	release := make(chan struct{})
	d := newTestDownloader(&Downloader{cfg: motionArtworkEnabledConfig(), logger: discardLogger()})
	d.catalog = blockingMotionCatalog{release: release}
	d.startMotionArtworkBackfill(context.Background(), "job-1", applemusic.ParsedURL{
		Storefront: "cn", Type: applemusic.TypeAlbum, ID: "1858184006",
	}, nil, &recordingReporter{})

	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := d.Wait(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait with the backfill in flight = %v, want the deadline", err)
	}
	close(release)
	long, cancelLong := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelLong()
	if err := d.Wait(long); err != nil {
		t.Fatalf("Wait after the backfill finished = %v, want nil", err)
	}
}

type blockingMotionCatalog struct {
	fakeDownloaderCatalog
	release <-chan struct{}
}

func (c blockingMotionCatalog) MotionArtworkViaWebToken(ctx context.Context, _, _ string) (applemusic.MotionArtwork, error) {
	select {
	case <-c.release:
		return applemusic.MotionArtwork{}, nil
	case <-ctx.Done():
		return applemusic.MotionArtwork{}, ctx.Err()
	}
}

// go-mp4tag stores the iTunes album and artist IDs as int32. Catalog IDs that
// outgrow it are left out of the tags rather than wrapped.
func TestITunesTagIDFitsInt32(t *testing.T) {
	ctx := context.Background()
	if got := itunesTagID(ctx, "album_id", "1858184006"); got != 1858184006 {
		t.Fatalf("itunesTagID(1858184006) = %d", got)
	}
	if got := itunesTagID(ctx, "album_id", "2147483647"); got != 2147483647 {
		t.Fatalf("itunesTagID(max int32) = %d", got)
	}
	if got := itunesTagID(ctx, "album_id", "2147483648"); got != 0 {
		t.Fatalf("itunesTagID(2^31) = %d, want 0 (omitted)", got)
	}
	if got := itunesTagID(ctx, "artist_id", ""); got != 0 {
		t.Fatalf("itunesTagID(empty) = %d, want 0", got)
	}
}
