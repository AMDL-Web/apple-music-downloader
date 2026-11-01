package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// quickTestALAC writes a 24-bit stereo ALAC file of 12 seconds, long enough
// that its middle packet lies outside the five seconds quick mode decodes at
// each end. Noise keeps every packet compressed rather than verbatim.
func quickTestALAC(t *testing.T) string {
	return quickTestALACOf(t, 12)
}

func quickTestALACOf(t *testing.T, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	path := filepath.Join(t.TempDir(), "flat.m4a")
	gen := exec.Command("ffmpeg", "-nostdin", "-y", "-v", "error",
		"-f", "lavfi", "-i", "anoisesrc=d="+strconv.Itoa(seconds)+":a=0.3:r=48000",
		"-ac", "2", "-sample_fmt", "s32p", "-c:a", "alac", path)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate ALAC: %v: %s", err, out)
	}
	return path
}

// breakMiddleALACTerminator clears the END element of the file's middle
// packet, the defect repairALACTerminators exists for, and returns the packet
// index.
func breakMiddleALACTerminator(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tracks, err := findALACRepairTracks(data)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("ALAC tracks = %d, err %v", len(tracks), err)
	}
	track := tracks[0]
	index := len(track.locs) / 2
	loc := track.locs[index]
	packet := data[loc.offset : loc.offset+int64(loc.size)]
	bodyEnd := findALACBodyEndBit(packet, &track.params)
	if bodyEnd < 0 || !alacEndTagAt(packet, bodyEnd) {
		t.Fatalf("packet %d has no END element to break (body end %d)", index, bodyEnd)
	}
	for bit := bodyEnd; bit < bodyEnd+3; bit++ {
		packet[bit/8] &^= 1 << uint(7-bit%8)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return index
}

func quickTestProcessor() *MP4Processor {
	cfg := config.Default()
	cfg.Tools.FFmpeg = "ffmpeg"
	return newMP4Processor(cfg)
}

// Quick mode must catch the one defect the gate is known to have caught in a
// real library: a single ALAC packet without its END element, away from the
// ends of the track. Decoding the ends alone misses it; the packet scan does
// not, and the repaired file passes both modes.
func TestQuickIntegrityCatchesABrokenALACTerminatorMidTrack(t *testing.T) {
	path := quickTestALAC(t)
	p := quickTestProcessor()
	ctx := context.Background()
	if err := p.checkIntegrityQuick(ctx, path); err != nil {
		t.Fatalf("quick check of a clean file: %v", err)
	}
	if err := p.checkIntegrityFile(ctx, path); err != nil {
		t.Fatalf("full check of a clean file: %v", err)
	}
	clean, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	index := breakMiddleALACTerminator(t, path)
	if err := p.checkIntegrityFile(ctx, path); err == nil {
		t.Fatal("full check passed a packet without its END element")
	}
	if err := p.decodeTrackEnds(ctx, path); err != nil {
		t.Fatalf("decoding the ends failed (%v); the broken packet must sit outside them for this test", err)
	}
	err = p.checkIntegrityQuick(ctx, path)
	if err == nil || !strings.Contains(err.Error(), "lack a clean END element") {
		t.Fatalf("quick check of packet %d without END = %v, want the terminator error", index, err)
	}

	broken, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	repaired, patched, err := repairALACTerminators(broken)
	if err != nil || patched != 1 {
		t.Fatalf("repair patched %d packets (err %v), want 1", patched, err)
	}
	if !bytes.Equal(repaired, clean) {
		t.Fatal("repaired file differs from the clean original")
	}
}

// A track shorter than the decoded windows (an interlude) passes: seeking
// 5 seconds before the end of a 2-second file decodes it from the start.
func TestQuickIntegrityPassesATrackShorterThanTheWindows(t *testing.T) {
	path := quickTestALACOf(t, 2)
	if err := quickTestProcessor().checkIntegrityQuick(context.Background(), path); err != nil {
		t.Fatalf("quick check of a 2-second track: %v", err)
	}
}

// Through finalizeTrackOutput, a quick-mode failure on an ALAC track takes the
// same repair path as a full-mode failure: one alac_repaired event, then a
// verified, committed track.
func TestQuickIntegrityRunsTheALACRepair(t *testing.T) {
	path := quickTestALAC(t)
	breakMiddleALACTerminator(t, path)
	cfg := config.Default()
	cfg.Tools.FFmpeg = "ffmpeg"
	cfg.Download.CheckIntegrity = true
	cfg.Download.IntegrityMode = config.IntegrityModeQuick
	d := newTestDownloader(&Downloader{cfg: cfg})
	reporter := &recordingReporter{}
	item := &domain.JobItem{ID: "item-1", JobID: "job-1"}
	out := filepath.Join(t.TempDir(), "Album", "01. Song.m4a")
	err := d.finalizeTrackOutput(context.Background(), sidecarOnlyMedia{realMedia{d: d}}, trackOutput{
		job: domain.Job{ID: "job-1"}, item: item, reporter: reporter, song: applemusic.Song{Name: "Song"}, codec: "alac",
		staged: stagedFile{path: path}, outPath: out, labels: enhancedOutputLabels, allowALACRepair: true,
	}, func(_ domain.ItemStatus, _ string, mutate func(*domain.ItemProgress)) {
		if mutate != nil {
			mutate(&item.Progress)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	repairs := 0
	for _, ev := range reporter.events {
		if ev.Type == domain.EventALACRepaired {
			repairs++
		}
	}
	if repairs != 1 || !item.Progress.Verified || item.Status != domain.ItemCompleted {
		t.Fatalf("repairs = %d, verified = %v, status = %s; want one repair and a verified, completed item",
			repairs, item.Progress.Verified, item.Status)
	}
}

// A file cut short, so its last samples point past the end, fails quick mode
// before ffmpeg runs.
func TestQuickIntegrityRejectsATruncatedFile(t *testing.T) {
	path := quickTestALAC(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// ffmpeg writes moov after mdat; shrink mdat's declared size as a writer
	// that stopped early would leave it, so the sample table overruns it.
	mdat := bytes.Index(data, []byte("mdat")) - 4
	if mdat < 0 {
		t.Fatal("no mdat box")
	}
	size := binary.BigEndian.Uint32(data[mdat:])
	binary.BigEndian.PutUint32(data[mdat:], size-4096)
	moovAt := mdat + int(size)
	// Keep the boxes contiguous: drop the bytes mdat no longer covers.
	data = append(data[:moovAt-4096:moovAt-4096], data[moovAt:]...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	err = quickTestProcessor().checkIntegrityQuick(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "outside the media data") {
		t.Fatalf("quick check of a truncated mdat = %v, want the sample layout error", err)
	}

	// A file missing its tail entirely fails on the box walk.
	cut := filepath.Join(t.TempDir(), "cut.m4a")
	if err := os.WriteFile(cut, data[:len(data)-100], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkSampleLayout(context.Background(), cut); err == nil {
		t.Fatal("quick check passed a file cut short")
	}
}
