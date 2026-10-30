package media

import (
	"errors"
	"fmt"
	"os"

	"github.com/Eyevinn/mp4ff/mp4"
)

// readSampleRate is the sample rate in Hz that the audio sample entry of the
// MP4 at path declares. It reads box headers only, never the audio data, so it
// costs nothing next to the decode check. The entry's rate field is 16 bits
// wide, which holds every rate a lossy stream has but not 96 kHz; ALAC takes its
// rate from the manifest instead.
func readSampleRate(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	file, err := mp4.DecodeFile(f, mp4.WithDecodeMode(mp4.DecModeLazyMdat))
	if err != nil {
		return 0, fmt.Errorf("read MP4 boxes: %w", err)
	}
	if file.Moov == nil {
		return 0, errors.New("no moov box")
	}
	for _, trak := range file.Moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
			continue
		}
		stsd := trak.Mdia.Minf.Stbl.Stsd
		for _, entry := range []*mp4.AudioSampleEntryBox{stsd.Mp4a, stsd.EC3, stsd.AC3} {
			if entry != nil && entry.SampleRate > 0 {
				return int(entry.SampleRate), nil
			}
		}
	}
	return 0, errors.New("no audio sample entry with a sample rate")
}
