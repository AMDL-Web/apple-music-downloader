package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	"amdl/internal/sysproc"
)

// quickDecodeSeconds is how much audio the quick integrity check decodes at
// each end of a track.
const quickDecodeSeconds = 5

// quickReadWindow is how much of the file the ALAC packet scan reads at a time,
// so the check never holds a whole track in memory.
const quickReadWindow = 1 << 20

// checkIntegrityQuick is download.integrity_mode quick: instead of decoding the
// whole track it checks what the full decode has caught, more cheaply.
//
//   - Every audio sample the sample table lists lies inside an mdat box within
//     the file, which catches a truncated file or a stale data offset.
//   - Every ALAC packet parses and closes with an END element, as ffmpeg's
//     decoder requires. This is the defect repairALACTerminators patches, and
//     it can sit in any single packet of a track.
//   - ffmpeg decodes the first and last quickDecodeSeconds cleanly, which
//     catches wrong keys or garbage that spoils every frame.
//
// AAC and Dolby tracks get the first and last check only, so damage confined
// to the middle of a lossy track passes.
func (p *MP4Processor) checkIntegrityQuick(ctx context.Context, path string) error {
	if err := checkSampleLayout(ctx, path); err != nil {
		return err
	}
	return p.decodeTrackEnds(ctx, path)
}

// decodeTrackEnds decodes the start and the end of the file in one ffmpeg run,
// one input per end, and fails on any error ffmpeg reports, like
// checkIntegrityFile.
func (p *MP4Processor) decodeTrackEnds(ctx context.Context, path string) error {
	window := strconv.Itoa(quickDecodeSeconds)
	cmd := exec.CommandContext(ctx, p.cfg.Tools.FFmpeg, "-nostdin", "-y", "-v", "error",
		"-threads", "1", "-t", window, "-i", path,
		"-threads", "1", "-sseof", "-"+window, "-i", path,
		"-map", "0:a:0", "-map", "1:a:0", "-c:a", "pcm_s16le", "-f", "null", "/dev/null")
	sysproc.NoConsole(cmd)
	out, err := cmd.CombinedOutput()
	diagnostic := toolDiagnostic(out)
	switch {
	case err != nil && diagnostic != "":
		return fmt.Errorf("ffmpeg decode of the first and last %ss: %w: %s", window, err, diagnostic)
	case err != nil:
		return fmt.Errorf("ffmpeg decode of the first and last %ss: %w", window, err)
	case diagnostic != "":
		return fmt.Errorf("ffmpeg decode of the first and last %ss reported errors: %s", window, diagnostic)
	}
	return nil
}

// checkSampleLayout reads the file's box layout and sample tables, checks that
// every audio sample lies inside an mdat box, and scans every ALAC packet.
func checkSampleLayout(ctx context.Context, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	moov, mdats, err := topLevelBoxes(f, size)
	if err != nil {
		return err
	}
	moovBytes := make([]byte, moov.end-moov.start)
	if _, err := f.ReadAt(moovBytes, moov.start); err != nil {
		return fmt.Errorf("read moov box: %w", err)
	}
	tracks, err := findSoundTracks(moovBytes)
	if err != nil {
		return err
	}
	if len(tracks) == 0 {
		return errors.New("no audio track")
	}
	window := &sampleWindow{f: f, size: size}
	for _, track := range tracks {
		locs, err := readALACPacketLocations(moovBytes, track.stbl)
		if err != nil {
			return fmt.Errorf("%s sample table: %w", track.format, err)
		}
		listed, err := stszSampleCount(moovBytes, track.stbl)
		if err != nil {
			return fmt.Errorf("%s sample table: %w", track.format, err)
		}
		if listed == 0 || len(locs) != listed {
			return fmt.Errorf("%s sample table lists %d samples but its chunks place %d", track.format, listed, len(locs))
		}
		for i, loc := range locs {
			if !insideBox(mdats, loc) {
				return fmt.Errorf("%s sample %d of %d (offset %d, %d bytes) lies outside the media data; the file is truncated or mis-offset",
					track.format, i+1, len(locs), loc.offset, loc.size)
			}
		}
		if track.format != "alac" {
			continue
		}
		params, err := extractALACRepairParams(moovBytes, track.entry)
		if err != nil {
			return err
		}
		if err := scanALACPackets(ctx, window, &params, locs); err != nil {
			return err
		}
	}
	return nil
}

// scanALACPackets checks that every packet parses and ends with an END element
// followed by at most one byte of padding: ffmpeg's decoder drops a packet
// without one and reports leftover bits beyond that.
func scanALACPackets(ctx context.Context, window *sampleWindow, params *alacRepairParams, locs []alacPacketLoc) error {
	unterminated, firstUnterminated := 0, 0
	for i, loc := range locs {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		packet, err := window.read(loc)
		if err != nil {
			return fmt.Errorf("read ALAC packet %d of %d: %w", i+1, len(locs), err)
		}
		bodyEnd := findALACBodyEndBit(packet, params)
		if bodyEnd < 0 {
			return fmt.Errorf("ALAC packet %d of %d does not parse", i+1, len(locs))
		}
		if alacEndTagAt(packet, bodyEnd) && len(packet)*8-(bodyEnd+3) <= 8 {
			continue
		}
		if unterminated == 0 {
			firstUnterminated = i + 1
		}
		unterminated++
	}
	if unterminated > 0 {
		return fmt.Errorf("%d ALAC packets of %d lack a clean END element (first: packet %d)", unterminated, len(locs), firstUnterminated)
	}
	return nil
}

// boxRange is a top-level box: start is its header, body its payload.
type boxRange struct {
	start, body, end int64
}

// topLevelBoxes walks the file's top-level boxes and returns the moov box and
// every mdat box. A box that runs past the end of the file means the file was
// cut short.
func topLevelBoxes(r io.ReaderAt, size int64) (boxRange, []boxRange, error) {
	var moov boxRange
	foundMoov := false
	var mdats []boxRange
	var header [16]byte
	for pos := int64(0); pos < size; {
		if size-pos < 8 {
			return moov, nil, fmt.Errorf("%d stray bytes at the end of the file", size-pos)
		}
		if _, err := r.ReadAt(header[:8], pos); err != nil {
			return moov, nil, fmt.Errorf("read box header at %d: %w", pos, err)
		}
		boxSize := int64(binary.BigEndian.Uint32(header[:4]))
		typ := string(header[4:8])
		headerSize := int64(8)
		switch boxSize {
		case 1:
			if size-pos < 16 {
				return moov, nil, fmt.Errorf("box %q at %d is cut short", typ, pos)
			}
			if _, err := r.ReadAt(header[8:16], pos+8); err != nil {
				return moov, nil, fmt.Errorf("read box header at %d: %w", pos, err)
			}
			large := binary.BigEndian.Uint64(header[8:16])
			if large > uint64(size) {
				return moov, nil, fmt.Errorf("box %q at %d runs past the end of the file; the file is truncated", typ, pos)
			}
			boxSize, headerSize = int64(large), 16
		case 0:
			boxSize = size - pos
		}
		if boxSize < headerSize {
			return moov, nil, fmt.Errorf("box %q at %d has an invalid size %d", typ, pos, boxSize)
		}
		if boxSize > size-pos {
			return moov, nil, fmt.Errorf("box %q at %d runs past the end of the file; the file is truncated", typ, pos)
		}
		box := boxRange{start: pos, body: pos + headerSize, end: pos + boxSize}
		switch typ {
		case "moov":
			moov, foundMoov = box, true
		case "mdat":
			mdats = append(mdats, box)
		}
		pos += boxSize
	}
	if !foundMoov {
		return moov, nil, errors.New("no moov box")
	}
	return moov, mdats, nil
}

func insideBox(boxes []boxRange, loc alacPacketLoc) bool {
	end := loc.offset + int64(loc.size)
	for _, box := range boxes {
		if loc.offset >= box.body && end <= box.end {
			return true
		}
	}
	return false
}

// stszSampleCount returns the number of samples the stsz box declares.
func stszSampleCount(data []byte, stbl mp4Atom) (int, error) {
	stsz, ok := findMP4Child(data, stbl.bodyOff, stbl.endOff, "stsz")
	if !ok || stsz.endOff-stsz.bodyOff < 12 {
		return 0, errors.New("stsz atom missing or too short")
	}
	return int(binary.BigEndian.Uint32(data[stsz.bodyOff+8 : stsz.bodyOff+12])), nil
}

// sampleWindow serves samples from a buffer refilled quickReadWindow bytes at
// a time; samples are read in file order, so most come from the buffer.
type sampleWindow struct {
	f     *os.File
	size  int64
	buf   []byte
	start int64
}

func (w *sampleWindow) read(loc alacPacketLoc) ([]byte, error) {
	end := loc.offset + int64(loc.size)
	if loc.offset >= w.start && end <= w.start+int64(len(w.buf)) {
		return w.buf[loc.offset-w.start : end-w.start], nil
	}
	length := int64(max(quickReadWindow, loc.size))
	length = min(length, w.size-loc.offset)
	if length < int64(loc.size) {
		return nil, io.ErrUnexpectedEOF
	}
	if int64(cap(w.buf)) < length {
		w.buf = make([]byte, length)
	}
	w.buf = w.buf[:length]
	if _, err := w.f.ReadAt(w.buf, loc.offset); err != nil {
		w.buf = w.buf[:0]
		return nil, err
	}
	w.start = loc.offset
	return w.buf[:loc.size], nil
}
