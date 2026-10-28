package media

// The buffered MP4 reference path: parse every sample into memory, decrypt,
// re-encapsulate and flatten through a temp file. Production streams one
// fragment at a time (streamDecryptToFlatFile); the round-trip tests decrypt
// and encode with this path in one shot and compare the two, and both share
// prepareDecryptedInit and encodeDecryptedFragment, so the plaintext fragment
// construction cannot drift.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Eyevinn/mp4ff/mp4"
)

type sampleInfo struct {
	DescIndex int
	Data      []byte
	Duration  int
}

// songInfo carries the parsed, still-encrypted media between extractSong and
// encapsulate. The mp4ff init segment and fragments are retained so the
// decrypted samples can be written back into the original container structure
// without re-parsing.
type songInfo struct {
	Codec     string
	Samples   []sampleInfo
	init      *mp4.InitSegment
	fragments []*mp4.Fragment
	// fragSampleCounts[i] is the number of samples in fragments[i]; used to map
	// the flat decrypted sample slice back onto each fragment's mdat.
	fragSampleCounts []int
}

// extractSong parses the encrypted fragmented MP4 downloaded from the CDN and
// pulls out the individual (still-encrypted) audio samples together with the
// per-fragment sample-description index used to select the decryption key.
// This replaces the previous gpac/MP4Box/mp4extract pipeline with pure-Go mp4ff
// box parsing.
func (p *MP4Processor) extractSong(_ context.Context, raw io.Reader, codec string) (songInfo, error) {
	// Buffer the reader so the many small mp4ff box reads don't each hit the
	// underlying *os.File; a bytes.Reader passed by tests is wrapped harmlessly.
	r := bufio.NewReaderSize(raw, 1<<20)
	var offset uint64
	init, offset, err := readInitSegment(r, offset)
	if err != nil {
		return songInfo{}, fmt.Errorf("read init segment: %w", err)
	}
	if init == nil || init.Moov == nil {
		return songInfo{}, errors.New("no moov box in init segment")
	}

	var trex *mp4.TrexBox
	if init.Moov.Mvex != nil && len(init.Moov.Mvex.Trexs) > 0 {
		trex = init.Moov.Mvex.Trexs[0]
	}
	defaultDescIndex := 1
	if trex != nil && trex.DefaultSampleDescriptionIndex > 0 {
		defaultDescIndex = int(trex.DefaultSampleDescriptionIndex)
	}

	var (
		samples   []sampleInfo
		fragments []*mp4.Fragment
		counts    []int
	)
	for {
		var frag *mp4.Fragment
		frag, offset, err = readNextFragment(r, offset)
		if err != nil {
			return songInfo{}, fmt.Errorf("read fragment: %w", err)
		}
		if frag == nil {
			break
		}
		full, err := frag.GetFullSamples(trex)
		if err != nil {
			return songInfo{}, fmt.Errorf("get fragment samples: %w", err)
		}

		// The sample-description index is signalled per fragment via tfhd; fall
		// back to the trex default when absent. It is 1-based in the container
		// and selects one of the (typically two) decryption keys.
		descIndex := defaultDescIndex
		if len(frag.Moof.Trafs) > 0 {
			tfhd := frag.Moof.Trafs[0].Tfhd
			if tfhd != nil && tfhd.HasSampleDescriptionIndex() {
				descIndex = int(tfhd.SampleDescriptionIndex)
			}
		}
		keyIndex := max(0, descIndex-1)

		for _, s := range full {
			samples = append(samples, sampleInfo{DescIndex: keyIndex, Data: s.Data, Duration: int(s.Dur)})
		}
		fragments = append(fragments, frag)
		counts = append(counts, len(full))
	}
	if len(samples) == 0 {
		return songInfo{}, errors.New("no samples extracted from media")
	}
	return songInfo{Codec: codec, Samples: samples, init: init, fragments: fragments, fragSampleCounts: counts}, nil
}

// encapsulate rebuilds a playable (fragmented) MP4 from the decrypted samples by
// stripping the encryption boxes from the init segment and fragments and writing
// the plaintext sample data back into each mdat. The decoder configuration
// (alac/mp4a/ec-3) is preserved from the original init segment, so no external
// muxer is required. The fragmented output is flattened to a regular MP4 by the
// subsequent ffmpeg copy pass (fixEncapsulate).
func (p *MP4Processor) encapsulate(_ context.Context, info songInfo, decrypted [][]byte) ([]byte, error) {
	if info.init == nil || info.init.Moov == nil {
		return nil, errors.New("missing init segment")
	}
	if len(decrypted) != len(info.Samples) {
		return nil, fmt.Errorf("decrypted sample count mismatch: got %d, want %d", len(decrypted), len(info.Samples))
	}

	if err := prepareDecryptedInit(info.init); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := info.init.Encode(&buf); err != nil {
		return nil, fmt.Errorf("encode init segment: %w", err)
	}

	sampleIdx := 0
	for fi, frag := range info.fragments {
		n := info.fragSampleCounts[fi]
		if err := encodeDecryptedFragment(&buf, fi, frag, decrypted[sampleIdx:sampleIdx+n]); err != nil {
			return nil, err
		}
		sampleIdx += n
	}
	if sampleIdx != len(decrypted) {
		return nil, fmt.Errorf("consumed %d of %d decrypted samples", sampleIdx, len(decrypted))
	}
	return buf.Bytes(), nil
}

// streamDecryptToFile reads the encrypted fragmented MP4 from r one fragment at
// a time, decrypts each fragment's samples via decrypt, and writes the plaintext
// fragmented MP4 to outPath. Only a single fragment's samples are held in memory
// at once, so peak memory is independent of track length. All samples in a
// fragment share one key, selected by the fragment's tfhd sample-description
// index (falling back to the trex default). onProgress, if non-nil, is called
// after each fragment with the number of encrypted input bytes consumed so far.
func (p *MP4Processor) streamDecryptToFile(
	ctx context.Context,
	r io.Reader,
	outPath string,
	keys []string,
	decrypt func(key string, samples [][]byte) ([][]byte, error),
	onProgress func(consumed uint64),
) error {
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	streamErr := p.streamDecryptToWriter(ctx, r, out, keys, decrypt, onProgress)
	closeErr := out.Close()
	if streamErr != nil {
		return streamErr
	}
	return closeErr
}

// flattenToFile flattens the fragmented MP4 in song into a regular progressive
// MP4 at outPath through a fix-* temp directory.
func (p *MP4Processor) flattenToFile(ctx context.Context, song []byte, outPath string) error {
	dir, err := os.MkdirTemp(p.cfg.Download.TempDir, "fix-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "in.m4a")
	if err := os.WriteFile(inPath, song, 0o644); err != nil {
		return err
	}
	return p.flattenFileToFile(ctx, inPath, outPath)
}

// flattenFileToFile flattens the fragmented MP4 at inPath into a regular
// progressive MP4 at outPath, with the same muxer flags produceToFlatFile uses.
func (p *MP4Processor) flattenFileToFile(ctx context.Context, inPath, outPath string) error {
	// -f mp4 is required: ffmpeg infers the muxer from the .m4a extension
	// otherwise, which selects the "ipod" muxer. That muxer's codec tag table
	// has no entry for eac3 (EC-3/Dolby Atmos), so it refuses to write the
	// header ("Could not find tag for codec eac3 ... not currently supported
	// in container") and every EC-3 download fails at this step. The generic
	// mp4 muxer supports the same tags plus ec-3; -brand "M4A " keeps the
	// ftyp major_brand the ipod muxer would have written (its default is
	// "isom" otherwise), matching the M4A brand callers document below.
	return run(ctx, p.cfg.Tools.FFmpeg, "-y", "-i", inPath, "-fflags", "+bitexact", "-map_metadata", "0", "-c:a", "copy", "-c:v", "copy", "-f", "mp4", "-brand", "M4A ", outPath)
}

func (p *MP4Processor) fixEncapsulate(ctx context.Context, song []byte) ([]byte, error) {
	dir, err := os.MkdirTemp(p.cfg.Download.TempDir, "fix-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	outPath := filepath.Join(dir, "out.m4a")
	if err := p.flattenToFile(ctx, song, outPath); err != nil {
		return nil, err
	}
	return os.ReadFile(outPath)
}

// checkIntegrity runs checkIntegrityFile over in-memory bytes.
func (p *MP4Processor) checkIntegrity(ctx context.Context, song []byte) error {
	dir, err := os.MkdirTemp(p.cfg.Download.TempDir, "check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "song.m4a")
	if err := os.WriteFile(inPath, song, 0o644); err != nil {
		return err
	}
	return p.checkIntegrityFile(ctx, inPath)
}

func run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
