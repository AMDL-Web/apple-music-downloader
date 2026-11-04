package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"github.com/Eyevinn/mp4ff/aac"
	"github.com/Eyevinn/mp4ff/mp4"
)

// writeAACInit writes an MP4 whose only track is AAC-LC at rate Hz.
func writeAACInit(t *testing.T, rate int) string {
	t.Helper()
	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(uint32(rate), "audio", "und")
	if err := trak.SetAACDescriptor(aac.AAClc, rate); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "flat.m4a")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := init.Encode(f); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSampleRateFromTheAudioSampleEntry(t *testing.T) {
	for _, rate := range []int{44100, 48000} {
		got, err := readSampleRate(writeAACInit(t, rate))
		if err != nil || got != rate {
			t.Errorf("readSampleRate = %d, %v; want %d", got, err, rate)
		}
	}
	notMP4 := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(notMP4, []byte("not an MP4 file at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readSampleRate(notMP4); err == nil {
		t.Errorf("readSampleRate on a non-MP4 = %d, nil; want an error", got)
	}
}

// A manifest's rate wins, and only an item without one is filled from the file.
func TestFinalizeFillsAMissingSampleRateOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		have int
		want int
	}{
		{name: "missing", have: 0, want: 44100},
		{name: "from the manifest", have: 96000, want: 96000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Download.CheckIntegrity = false
			d := newTestDownloader(&Downloader{cfg: cfg})
			item := &domain.JobItem{ID: "item-1", SampleRate: tc.have}
			out := filepath.Join(t.TempDir(), "Album", "01. Song.m4a")
			err := d.finalizeTrackOutput(context.Background(), sidecarOnlyMedia{realMedia{d: d}}, trackOutput{
				item: item, song: applemusic.Song{Name: "Song"}, codec: "aac",
				staged: stagedFile{path: writeAACInit(t, 44100)}, outPath: out, labels: enhancedOutputLabels,
			}, noopStage)
			if err != nil {
				t.Fatal(err)
			}
			if item.SampleRate != tc.want {
				t.Fatalf("sample rate = %d, want %d", item.SampleRate, tc.want)
			}
		})
	}
}

// writeALACInit writes an MP4 whose only track is 24-bit ALAC at 48 kHz.
func writeALACInit(t *testing.T) string {
	t.Helper()
	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(48000, "audio", "und")
	cookie := &mp4.AlacBox{FrameLength: 4096, BitDepth: 24, Pb: 40, Mb: 10, Kb: 14, NumChannels: 2, MaxRun: 255, SampleRate: 48000}
	trak.Mdia.Minf.Stbl.Stsd.AddChild(mp4.CreateAudioSampleEntryBox("alac", 2, 24, 0, cookie))
	path := filepath.Join(t.TempDir(), "flat.m4a")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := init.Encode(f); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSampleEntryNamesTheAudioCodecKind(t *testing.T) {
	if got, err := readSampleEntry(writeALACInit(t)); err != nil || got != "alac" {
		t.Errorf("ALAC file: readSampleEntry = %q, %v; want alac", got, err)
	}
	if got, err := readSampleEntry(writeAACInit(t, 44100)); err != nil || got != "mp4a" {
		t.Errorf("AAC file: readSampleEntry = %q, %v; want mp4a", got, err)
	}
	notMP4 := filepath.Join(t.TempDir(), "flat.m4a")
	if err := os.WriteFile(notMP4, []byte("hand placed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readSampleEntry(notMP4); err == nil {
		t.Errorf("readSampleEntry on a non-MP4 = %q, nil; want an error", got)
	}
}

// An adopted file keeps the selected codec when it is of that codec's kind,
// takes the one codec of its own kind when it is not, and stays unknown when
// its kind (AAC) has several codecs or it could not be read.
func TestAdoptedCodecReadsTheFileKind(t *testing.T) {
	for _, tc := range []struct {
		entry, selected, codec string
		same                   bool
	}{
		{"alac", "alac", "alac", true},
		{"mp4a", "aac-lc", "aac-lc", true},
		{"mp4a", "aac", "aac", true},
		{"ec-3", "ec3", "ec3", true},
		{"alac", "aac-lc", "alac", false},
		{"ec-3", "alac", "ec3", false},
		{"ac-3", "alac", "ac3", false},
		{"mp4a", "alac", "", false},
		{"", "alac", "", false},
	} {
		if codec, same := adoptedCodec(tc.entry, tc.selected); codec != tc.codec || same != tc.same {
			t.Errorf("adoptedCodec(%q, %q) = %q, %v; want %q, %v", tc.entry, tc.selected, codec, same, tc.codec, tc.same)
		}
	}
}
