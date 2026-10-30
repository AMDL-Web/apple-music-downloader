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
