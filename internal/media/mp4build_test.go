package media

import (
	"bytes"
	"os"
	"testing"

	"github.com/Eyevinn/mp4ff/mp4"
)

// codedSample is one coded audio access unit and its duration in media
// timescale ticks.
type codedSample struct {
	data []byte
	dur  uint32
}

// readAudioSamples returns the coded samples of the first track of a
// fragmented or progressive MP4 file, independent of any decoder, so two
// files can be compared for lossless remuxing.
func readAudioSamples(t *testing.T, path string) []codedSample {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := mp4.DecodeFile(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	if file.IsFragmented() {
		var trex *mp4.TrexBox
		if file.Init != nil && file.Init.Moov.Mvex != nil {
			trex = file.Init.Moov.Mvex.Trex
		}
		var out []codedSample
		for _, seg := range file.Segments {
			for _, frag := range seg.Fragments {
				full, err := frag.GetFullSamples(trex)
				if err != nil {
					t.Fatalf("%s: fragment samples: %v", path, err)
				}
				for _, s := range full {
					out = append(out, codedSample{data: s.Data, dur: s.Dur})
				}
			}
		}
		return out
	}
	if file.Moov == nil || len(file.Moov.Traks) == 0 {
		t.Fatalf("%s: no moov/trak", path)
	}
	stbl := file.Moov.Traks[0].Mdia.Minf.Stbl
	var durs []uint32
	for i, n := range stbl.Stts.SampleCount {
		for range n {
			durs = append(durs, stbl.Stts.SampleTimeDelta[i])
		}
	}
	count := int(stbl.Stsz.GetNrSamples())
	if len(durs) != count {
		t.Fatalf("%s: stts covers %d samples, stsz %d", path, len(durs), count)
	}
	out := make([]codedSample, 0, count)
	for nr := 1; nr <= count; nr++ {
		chunk, first, err := stbl.Stsc.ChunkNrFromSampleNr(nr)
		if err != nil {
			t.Fatalf("%s: sample %d chunk: %v", path, nr, err)
		}
		var off uint64
		if stbl.Co64 != nil {
			off, err = stbl.Co64.GetOffset(chunk)
		} else {
			off, err = stbl.Stco.GetOffset(chunk)
		}
		if err != nil {
			t.Fatalf("%s: chunk %d offset: %v", path, chunk, err)
		}
		for s := first; s < nr; s++ {
			off += uint64(stbl.Stsz.GetSampleSize(s))
		}
		size := uint64(stbl.Stsz.GetSampleSize(nr))
		if off+size > uint64(len(raw)) {
			t.Fatalf("%s: sample %d out of bounds", path, nr)
		}
		out = append(out, codedSample{data: raw[off : off+size], dur: durs[nr-1]})
	}
	return out
}
