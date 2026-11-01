package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// quickTestAAC writes a stereo AAC-LC file of the given length. Noise keeps
// every packet full-sized.
func quickTestAAC(t *testing.T, seconds int) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	path := filepath.Join(t.TempDir(), "lossy.m4a")
	gen := exec.Command("ffmpeg", "-nostdin", "-y", "-v", "error",
		"-f", "lavfi", "-i", "anoisesrc=d="+strconv.Itoa(seconds)+":a=0.3:r=44100",
		"-ac", "2", "-c:a", "aac", "-b:a", "128k", path)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate AAC: %v: %s", err, out)
	}
	return path
}

// quickFixture is a generated file read into memory with the places tests
// damage: the moov box and the audio packets' locations.
type quickFixture struct {
	path string
	data []byte
	moov boxRange
	locs []alacPacketLoc
}

func loadQuickFixture(t *testing.T, path string) *quickFixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	moov, _, err := topLevelBoxes(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	moovBytes := data[moov.start:moov.end]
	tracks, err := findSoundTracks(moovBytes)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("sound tracks = %d, err %v", len(tracks), err)
	}
	locs, err := readALACPacketLocations(moovBytes, tracks[0].stbl)
	if err != nil || len(locs) < 20 {
		t.Fatalf("packets = %d, err %v", len(locs), err)
	}
	return &quickFixture{path: path, data: data, moov: moov, locs: locs}
}

// save writes the fixture's (possibly damaged) bytes to a new file and
// returns its path.
func (f *quickFixture) save(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "damaged.m4a")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// atom returns the offset in the file of the nth (0-based) occurrence of a
// four-character box type inside the moov box.
func (f *quickFixture) atom(t *testing.T, typ string, nth int) int {
	t.Helper()
	moov := f.data[f.moov.start:f.moov.end]
	from := 0
	for {
		i := bytes.Index(moov[from:], []byte(typ))
		if i < 0 {
			t.Fatalf("no %q box #%d in moov", typ, nth)
		}
		if nth == 0 {
			return int(f.moov.start) + from + i
		}
		nth--
		from += i + len(typ)
	}
}

// damaged returns a copy of the file's bytes after apply edits them.
func (f *quickFixture) damaged(apply func(data []byte)) []byte {
	data := append([]byte(nil), f.data...)
	apply(data)
	return data
}

// garble overwrites a packet with bytes ffmpeg's decoders reject.
func garble(data []byte, loc alacPacketLoc) {
	for i := 0; i < loc.size; i++ {
		data[loc.offset+int64(i)] = 0xFF
	}
}

func boxHeader(typ string, size uint32) []byte {
	h := make([]byte, 8)
	binary.BigEndian.PutUint32(h, size)
	copy(h[4:], typ)
	return h
}

// Every way a sample table can disagree with the file fails quick mode with an
// error naming the defect, before ffmpeg is asked to decode anything.
func TestQuickIntegrityRejectsADamagedSampleTable(t *testing.T) {
	f := loadQuickFixture(t, quickTestALACOf(t, 2))
	p := quickTestProcessor()
	put32 := func(data []byte, at int, v uint32) { binary.BigEndian.PutUint32(data[at:], v) }
	cases := []struct {
		name  string
		apply func(data []byte)
		want  string
	}{
		{"no audio track", func(d []byte) { copy(d[f.atom(t, "soun", 0):], "vide") }, "no audio track"},
		{"stsz missing", func(d []byte) { copy(d[f.atom(t, "stsz", 0):], "free") }, "alac sample table: stsz atom missing"},
		{"stsc missing", func(d []byte) { copy(d[f.atom(t, "stsc", 0):], "free") }, "alac sample table: stsc atom missing"},
		{"stco missing", func(d []byte) { copy(d[f.atom(t, "stco", 0):], "free") }, "alac sample table: stco/co64 atom missing"},
		{"stsz lists no samples", func(d []byte) { put32(d, f.atom(t, "stsz", 0)+12, 0) }, "lists 0 samples"},
		{"chunks place no samples", func(d []byte) { put32(d, f.atom(t, "stco", 0)+8, 0) }, "its chunks place 0"},
		{"stsz count beyond its table", func(d []byte) { put32(d, f.atom(t, "stsz", 0)+12, uint32(len(f.locs)*50)) }, "sample table"},
		{"ALAC config missing", func(d []byte) { copy(d[f.atom(t, "alac", 1):], "free") }, "missing ALAC config atom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := f.save(t, f.damaged(tc.apply))
			err := p.checkIntegrityQuick(context.Background(), path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("quick check = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

// The box walk fails a file whose top-level structure is damaged, and accepts
// the legal shapes ffmpeg and other muxers write: a box running to the end of
// the file (size 0), 64-bit box sizes, even on moov itself.
func TestQuickIntegrityWalksTopLevelBoxes(t *testing.T) {
	f := loadQuickFixture(t, quickTestALACOf(t, 2))
	p := quickTestProcessor()
	large := func(typ string, payload int) []byte {
		b := boxHeader(typ, 1)
		b = binary.BigEndian.AppendUint64(b, uint64(16+payload))
		return append(b, make([]byte, payload)...)
	}
	appended := func(extra []byte) func([]byte) []byte {
		return func(d []byte) []byte { return append(d, extra...) }
	}
	cases := []struct {
		name  string
		build func(data []byte) []byte
		want  string // empty: the file passes
	}{
		{"stray bytes after the last box", appended([]byte{1, 2, 3}), "stray bytes"},
		{"box smaller than its header", appended(boxHeader("junk", 4)), `box "junk"`},
		{"64-bit size header cut short", appended(boxHeader("junk", 1)), "cut short"},
		{"64-bit size beyond the file", appended(append(boxHeader("junk", 1), binary.BigEndian.AppendUint64(nil, 1<<40)...)), "runs past the end"},
		{"box beyond the file", appended(boxHeader("junk", 1<<20)), "runs past the end"},
		{"no moov box", func(d []byte) []byte { return d[:f.moov.start] }, "no moov box"},
		{"empty file", func([]byte) []byte { return nil }, "no moov box"},
		{"box running to the end of the file", appended(append(boxHeader("free", 0), make([]byte, 10)...)), ""},
		{"64-bit sized box", appended(large("free", 4)), ""},
		{"moov with a 64-bit size", func(d []byte) []byte {
			head := binary.BigEndian.AppendUint64(boxHeader("moov", 1), uint64(f.moov.end-f.moov.start)+8)
			return append(append(append([]byte(nil), d[:f.moov.start]...), head...), d[f.moov.body:]...)
		}, ""},
		{"moov running to the end of the file", func(d []byte) []byte {
			binary.BigEndian.PutUint32(d[f.moov.start:], 0)
			return d
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := f.save(t, tc.build(append([]byte(nil), f.data...)))
			err := p.checkIntegrityQuick(context.Background(), path)
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("quick check = %v, want a pass", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("quick check = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestQuickIntegrityReportsAMissingFile(t *testing.T) {
	err := quickTestProcessor().checkIntegrityQuick(context.Background(), filepath.Join(t.TempDir(), "gone.m4a"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("quick check of a missing file = %v, want not-exist", err)
	}
}

// A packet whose first element tag is not one ALAC defines cannot be parsed.
// It sits away from the ends, so only the packet scan sees it, and the error
// says which packet.
func TestQuickIntegrityRejectsAnUnparsableALACPacket(t *testing.T) {
	f := loadQuickFixture(t, quickTestALAC(t))
	index := len(f.locs) / 2
	loc := f.locs[index]
	path := f.save(t, f.damaged(func(d []byte) { d[loc.offset] = d[loc.offset]&0x1f | 0xA0 }))
	p := quickTestProcessor()
	if err := p.decodeTrackEnds(context.Background(), path); err != nil {
		t.Fatalf("decoding the ends failed (%v); the damaged packet must sit outside them", err)
	}
	err := p.checkIntegrityQuick(context.Background(), path)
	want := "ALAC packet " + strconv.Itoa(index+1) + " of " + strconv.Itoa(len(f.locs)) + " does not parse"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("quick check = %v, want %q", err, want)
	}
}

// Several packets without their END element are counted, and the first is named.
func TestQuickIntegrityCountsUnterminatedALACPackets(t *testing.T) {
	f := loadQuickFixture(t, quickTestALAC(t))
	tracks, err := findALACRepairTracks(f.data)
	if err != nil || len(tracks) != 1 {
		t.Fatalf("ALAC tracks = %d, err %v", len(tracks), err)
	}
	params := tracks[0].params
	first := len(f.locs) / 2
	path := f.save(t, f.damaged(func(d []byte) {
		for _, index := range []int{first, first + 3, first + 9} {
			loc := f.locs[index]
			packet := d[loc.offset : loc.offset+int64(loc.size)]
			bodyEnd := findALACBodyEndBit(packet, &params)
			for bit := bodyEnd; bit < bodyEnd+3; bit++ {
				packet[bit/8] &^= 1 << uint(7-bit%8)
			}
		}
	}))
	err = quickTestProcessor().checkIntegrityQuick(context.Background(), path)
	want := "3 ALAC packets of " + strconv.Itoa(len(f.locs)) + " lack a clean END element (first: packet " + strconv.Itoa(first+1) + ")"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("quick check = %v, want %q", err, want)
	}
}

// A cancelled context stops the check with the cancellation, in the packet scan
// and in the ffmpeg run, and the check does not pass.
func TestQuickIntegrityStopsWhenCancelled(t *testing.T) {
	path := quickTestALACOf(t, 2)
	p := quickTestProcessor()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.checkIntegrityQuick(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("quick check with a cancelled context = %v, want context.Canceled", err)
	}
	if err := p.decodeTrackEnds(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("ffmpeg run with a cancelled context = %v, want context.Canceled", err)
	}
}

// ffmpeg that is not installed fails the check; an integrity check that cannot
// run must never pass a file.
func TestQuickIntegrityFailsWhenFFmpegIsMissing(t *testing.T) {
	path := quickTestALACOf(t, 2)
	cfg := config.Default()
	cfg.Tools.FFmpeg = "amdl-no-such-ffmpeg"
	err := newMP4Processor(cfg).checkIntegrityQuick(context.Background(), path)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("quick check without ffmpeg = %v, want exec.ErrNotFound", err)
	}
}

// The ffmpeg run's verdict, with a stand-in ffmpeg: a clean exit passes, any
// diagnostic or failing exit does not, and a long diagnostic is capped so the
// item's error stays readable.
func TestDecodeTrackEndsVerdicts(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   string // empty: passes
	}{
		{"clean exit", `exit 0`, ""},
		{"warning on a clean exit", `echo "Invalid data found" >&2; exit 0`, "reported errors: Invalid data found"},
		{"failing exit with a diagnostic", `echo "Invalid data found" >&2; exit 1`, "exit status 1: Invalid data found"},
		{"failing exit without a diagnostic", `exit 3`, "first and last 5s: exit status 3"},
		{"endless diagnostic", `head -c 100000 /dev/zero | tr '\0' x >&2; exit 1`, "more bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newPipeTestProcessor(t, writePipeTestCommand(t, tc.script))
			err := p.decodeTrackEnds(context.Background(), "/media/does-not-matter.m4a")
			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("decodeTrackEnds = %v, want a pass", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("decodeTrackEnds = %v, want an error containing %q", err, tc.want)
			case err != nil && len(err.Error()) > maxToolDiagnosticBytes+200:
				t.Fatalf("error is %d bytes, want it capped near %d", len(err.Error()), maxToolDiagnosticBytes)
			}
		})
	}
}

// Both ends of the track are decoded: the file is given to ffmpeg once for the
// start and once for the end.
func TestDecodeTrackEndsOpensTheFileTwice(t *testing.T) {
	record := filepath.Join(t.TempDir(), "args")
	p := newPipeTestProcessor(t, writePipeTestCommand(t, `printf '%s\n' "$@" > "`+record+`"`))
	if err := p.decodeTrackEnds(context.Background(), "/media/track.m4a"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(args), "/media/track.m4a\n"); n != 2 {
		t.Fatalf("ffmpeg was given the file %d times, want 2 (start and end); args:\n%s", n, args)
	}
}

// A hung ffmpeg does not hang the check: the context deadline kills it.
func TestDecodeTrackEndsStopsAtTheDeadline(t *testing.T) {
	p := newPipeTestProcessor(t, writePipeTestCommand(t, `exec sleep 60`))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := p.decodeTrackEnds(ctx, "/media/track.m4a"); err == nil {
		t.Fatal("decodeTrackEnds passed although ffmpeg was killed")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("decodeTrackEnds took %v after a 200ms deadline", elapsed)
	}
}

// A lossy track gets the sample layout check and the two ffmpeg decodes. Damage
// at either end fails it; damage confined to the middle passes quick mode and
// is what full mode is for (the documented limit of quick mode).
func TestQuickIntegrityOnALossyTrack(t *testing.T) {
	f := loadQuickFixture(t, quickTestAAC(t, 20))
	p := quickTestProcessor()
	ctx := context.Background()
	if err := p.checkIntegrityQuick(ctx, f.path); err != nil {
		t.Fatalf("quick check of a clean AAC track: %v", err)
	}
	if err := p.checkIntegrityFile(ctx, f.path); err != nil {
		t.Fatalf("full check of a clean AAC track: %v", err)
	}
	damageRange := func(from, to int) string {
		return f.save(t, f.damaged(func(d []byte) {
			for _, loc := range f.locs[from:to] {
				garble(d, loc)
			}
		}))
	}
	mid := len(f.locs) / 2
	for _, tc := range []struct {
		name         string
		path         string
		quickCatches bool
	}{
		{"start", damageRange(0, 20), true},
		{"end", damageRange(len(f.locs)-20, len(f.locs)), true},
		{"middle", damageRange(mid-10, mid+10), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := p.checkIntegrityFile(ctx, tc.path); err == nil {
				t.Fatal("full check passed the damaged track; the fixture does not damage what ffmpeg reports")
			}
			err := p.checkIntegrityQuick(ctx, tc.path)
			switch {
			case tc.quickCatches && (err == nil || !strings.Contains(err.Error(), "first and last 5s")):
				t.Fatalf("quick check of damage at the %s = %v, want the ffmpeg decode error", tc.name, err)
			case !tc.quickCatches && err != nil:
				t.Fatalf("quick check of damage in the %s = %v, want a pass", tc.name, err)
			}
		})
	}
}

func finalizeRealFile(t *testing.T, staged string, allowRepair bool) (*domain.JobItem, *recordingReporter, string, error) {
	t.Helper()
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
		staged: stagedFile{path: staged}, outPath: out, labels: enhancedOutputLabels, allowALACRepair: allowRepair,
	}, func(_ domain.ItemStatus, _ string, mutate func(*domain.ItemProgress)) {
		if mutate != nil {
			mutate(&item.Progress)
		}
	})
	return item, reporter, out, err
}

// With the repair unavailable, a real file quick mode flags and a real full
// decode confirms fails the track with the quick error, writes no output and
// raises no overruled event.
func TestQuickFailureOnARealCorruptFileFailsTheTrack(t *testing.T) {
	path := quickTestALAC(t)
	breakMiddleALACTerminator(t, path)
	item, reporter, out, err := finalizeRealFile(t, path, false)
	if err == nil || !strings.Contains(err.Error(), "lack a clean END element") {
		t.Fatalf("finalize = %v, want the quick check's terminator error", err)
	}
	if item.Progress.Verified || item.Status == domain.ItemCompleted {
		t.Fatalf("verified = %v, status = %s; a corrupt track must not complete", item.Progress.Verified, item.Status)
	}
	if n := len(eventsOfType(reporter, domain.EventIntegrityOverruled)); n != 0 {
		t.Fatalf("integrity_overruled events = %d, want none", n)
	}
	if _, err := os.Stat(out); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("output after a failed check: stat err = %v, want not-exist", err)
	}
}

// A file whose audio the full decode reads fine but whose mdat box is gone from
// the box list (renamed, as some muxers and editors leave it) is a quick-mode
// false alarm: the full decode clears it and the track completes, with one
// integrity_overruled event.
func TestQuickFalseAlarmOnARealFileIsClearedByTheFullDecode(t *testing.T) {
	f := loadQuickFixture(t, quickTestALAC(t))
	mdat := bytes.Index(f.data[:f.moov.start], []byte("mdat"))
	if mdat < 0 {
		t.Fatal("no mdat box")
	}
	path := f.save(t, f.damaged(func(d []byte) { copy(d[mdat:], "free") }))
	p := quickTestProcessor()
	if err := p.checkIntegrityFile(context.Background(), path); err != nil {
		t.Skipf("this ffmpeg does not decode a file without an mdat box (%v); no false alarm to reproduce", err)
	}
	if err := p.checkIntegrityQuick(context.Background(), path); err == nil || !strings.Contains(err.Error(), "outside the media data") {
		t.Fatalf("quick check = %v, want the sample layout error", err)
	}
	item, reporter, out, err := finalizeRealFile(t, path, false)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if n := len(eventsOfType(reporter, domain.EventIntegrityOverruled)); n != 1 {
		t.Fatalf("integrity_overruled events = %d, want 1", n)
	}
	if !item.Progress.Verified || item.Status != domain.ItemCompleted {
		t.Fatalf("verified = %v, status = %s; want a verified, completed item", item.Progress.Verified, item.Status)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("output of the cleared track: %v", err)
	}
}
