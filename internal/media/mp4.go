package media

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/logging"
	"github.com/Eyevinn/mp4ff/mp4"
	"github.com/zhaarey/go-mp4tag"
)

type MP4Processor struct {
	cfg config.Config
}

func newMP4Processor(cfg config.Config) *MP4Processor {
	return &MP4Processor{cfg: cfg}
}

// prepareDecryptedInit rewrites an init segment for plaintext output: it converts
// the encrypted sample entries (enca -> alac/mp4a/ec-3) and drops sinf/pssh via
// mp4ff's DecryptInit, removes the encryption sample groups (seig) left in the
// sample table, and collapses Apple's two identical (post-decryption) sample
// entries down to one. The buffered reference path in the tests shares it with
// streamDecryptToWriter, so the two emit an identical fragmented container.
func prepareDecryptedInit(init *mp4.InitSegment) error {
	if _, err := mp4.DecryptInit(init); err != nil {
		return fmt.Errorf("decrypt init: %w", err)
	}
	for _, trak := range init.Moov.Traks {
		if trak.Mdia != nil && trak.Mdia.Minf != nil && trak.Mdia.Minf.Stbl != nil {
			stbl := trak.Mdia.Minf.Stbl
			stbl.Children = filterEncryptionSampleGroups(stbl.Children)
		}
	}
	// Apple Music encrypts with two identical sample entries (two IVs); after
	// decryption they are identical, so collapse to a single entry and point
	// every fragment at it.
	collapseSampleEntries(init)
	return nil
}

// encodeDecryptedFragment writes the fragment's plaintext sample data back into
// its mdat, strips the per-fragment encryption boxes (senc/saiz/saio) and pssh,
// fixes trun.data_offset for the removed bytes, and encodes the fragment to w.
// len(decrypted) must equal the fragment's sample count. fi is used only for
// error messages.
func encodeDecryptedFragment(w io.Writer, fi int, frag *mp4.Fragment, decrypted [][]byte) error {
	if frag.Mdat == nil {
		return fmt.Errorf("fragment %d has no mdat box", fi)
	}
	// Keep the wrapper's per-sample reply buffers as separate mdat parts. mp4ff
	// writes DataParts sequentially, so concatenating them into another
	// fragment-sized []byte only increases the peak heap without changing the
	// encoded container. Clear the encrypted payload first: Data and DataParts
	// are mutually exclusive representations of the same mdat body.
	frag.Mdat.Data = nil
	frag.Mdat.DataParts = decrypted

	// Strip per-fragment encryption boxes (senc/saiz/saio) and pssh, then shrink
	// trun.data_offset by the removed byte count. mp4ff Encode does NOT recompute
	// data_offset when it is already non-zero — leaving the pre-strip value makes
	// the ffmpeg flatten read past the real mdat payload (observed as leading
	// zeroed ALAC packets after flattening).
	var bytesRemoved uint64
	for _, traf := range frag.Moof.Trafs {
		bytesRemoved += traf.RemoveEncryptionBoxes()
		// Normalise the sample-description index to the single retained entry.
		if traf.Tfhd != nil && traf.Tfhd.HasSampleDescriptionIndex() {
			traf.Tfhd.SampleDescriptionIndex = 1
		}
	}
	_, psshRemoved := frag.Moof.RemovePsshs()
	bytesRemoved += psshRemoved
	if bytesRemoved > math.MaxInt32 {
		return fmt.Errorf("fragment %d removed too many bytes for trun.data_offset adjustment: %d", fi, bytesRemoved)
	}
	removed := int32(bytesRemoved)
	for _, traf := range frag.Moof.Trafs {
		for _, trun := range traf.Truns {
			if trun.HasDataOffset() {
				trun.DataOffset -= removed
			}
		}
	}
	if err := frag.Encode(w); err != nil {
		return fmt.Errorf("encode fragment %d: %w", fi, err)
	}
	return nil
}

// streamDecryptToWriter is the common fragment-at-a-time decrypt/remux loop.
// The production path connects it directly to ffmpeg's stdin so the decrypted
// fragmented MP4 never exists as either a whole-track []byte or an intermediate
// file, regardless of whether the encrypted input is memory- or disk-backed.
func (p *MP4Processor) streamDecryptToWriter(
	ctx context.Context,
	r io.Reader,
	w io.Writer,
	keys []string,
	decrypt func(key string, samples [][]byte) ([][]byte, error),
	onProgress func(consumed uint64),
) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	br := bufio.NewReaderSize(r, 1<<20)

	var offset uint64
	init, offset, err := readInitSegment(br, offset)
	if err != nil {
		return fmt.Errorf("read init segment: %w", err)
	}
	if init == nil || init.Moov == nil {
		return errors.New("no moov box in init segment")
	}

	// Capture trex and the default sample-description index BEFORE
	// prepareDecryptedInit runs: collapseSampleEntries rewrites the trex default
	// index to 1, but the per-fragment key selection below needs the original
	// value so it picks the same key the buffered path would.
	var trex *mp4.TrexBox
	if init.Moov.Mvex != nil && len(init.Moov.Mvex.Trexs) > 0 {
		trex = init.Moov.Mvex.Trexs[0]
	}
	defaultDescIndex := 1
	if trex != nil && trex.DefaultSampleDescriptionIndex > 0 {
		defaultDescIndex = int(trex.DefaultSampleDescriptionIndex)
	}

	if err := prepareDecryptedInit(init); err != nil {
		return err
	}
	if err := init.Encode(bw); err != nil {
		return fmt.Errorf("encode init segment: %w", err)
	}

	fragCount := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var frag *mp4.Fragment
		frag, offset, err = readNextFragment(br, offset)
		if err != nil {
			return fmt.Errorf("read fragment: %w", err)
		}
		if frag == nil {
			break
		}
		full, err := frag.GetFullSamples(trex)
		if err != nil {
			return fmt.Errorf("get fragment samples: %w", err)
		}

		descIndex := defaultDescIndex
		if len(frag.Moof.Trafs) > 0 {
			tfhd := frag.Moof.Trafs[0].Tfhd
			if tfhd != nil && tfhd.HasSampleDescriptionIndex() {
				descIndex = int(tfhd.SampleDescriptionIndex)
			}
		}
		keyIndex := max(0, descIndex-1)
		if keyIndex >= len(keys) {
			keyIndex = 0
		}
		key := ""
		if len(keys) > 0 {
			key = keys[keyIndex]
		}

		sampleData := make([][]byte, len(full))
		for i := range full {
			sampleData[i] = full[i].Data
		}
		decrypted, err := decrypt(key, sampleData)
		if err != nil {
			return err
		}
		if len(decrypted) != len(full) {
			return fmt.Errorf("fragment %d: decrypted %d samples, want %d", fragCount, len(decrypted), len(full))
		}
		if err := encodeDecryptedFragment(bw, fragCount, frag, decrypted); err != nil {
			return err
		}
		fragCount++
		if onProgress != nil {
			onProgress(offset)
		}
	}
	if fragCount == 0 {
		return errors.New("no fragments extracted from media")
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return nil
}

// produceToFlatFile connects a plaintext fragmented-MP4 producer directly to
// ffmpeg's stdin and waits for the progressive M4A output to be finalized.
// StdinPipe deliberately provides kernel backpressure: if ffmpeg exits early,
// the producer's next write fails instead of an unbounded goroutine or buffer
// retaining a whole track. cmd.Wait is called on every path after Start, even
// when production, stdin close, or the context fails, so the child is reaped
// and both producer and ffmpeg failures remain in the returned error chain.
//
// afterInput is invoked only after the producer succeeded and stdin was closed,
// but before cmd.Wait. Callers use this exact boundary to publish remuxing while
// ffmpeg is flushing its muxer trailer rather than after remuxing has finished.
func (p *MP4Processor) produceToFlatFile(
	ctx context.Context,
	outPath string,
	produce func(io.Writer) error,
	afterInput func(),
) error {
	if produce == nil {
		return errors.New("nil streamed flatten producer")
	}
	cmd := exec.CommandContext(ctx, p.cfg.Tools.FFmpeg,
		"-y", "-f", "mp4", "-i", "pipe:0",
		"-fflags", "+bitexact", "-map_metadata", "0",
		"-c:a", "copy", "-c:v", "copy", "-f", "mp4", "-brand", "M4A ", outPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open streamed flatten stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start streamed flatten: %w", err)
	}

	produceErr := produce(stdin)
	closeErr := stdin.Close()
	if produceErr == nil && closeErr == nil && afterInput != nil {
		afterInput()
	}
	waitErr := cmd.Wait()
	if waitErr != nil && ctx.Err() != nil {
		// CommandContext reports the process-level failure (usually signal:
		// killed). Preserve it while also making errors.Is(err, ctx.Err()) work
		// for callers deciding whether a retry should be suppressed.
		waitErr = fmt.Errorf("%w (process wait: %w)", ctx.Err(), waitErr)
	}

	if produceErr != nil && closeErr != nil {
		produceErr = fmt.Errorf("produce media: %w; close streamed flatten stdin: %w", produceErr, closeErr)
	} else if closeErr != nil {
		produceErr = fmt.Errorf("close streamed flatten stdin: %w", closeErr)
	}

	if produceErr != nil {
		if waitErr != nil {
			return fmt.Errorf("stream media: %w; streamed flatten: %w: %s", produceErr, waitErr, strings.TrimSpace(stderr.String()))
		}
		return produceErr
	}
	if waitErr != nil {
		return fmt.Errorf("streamed flatten: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// streamDecryptToFlatFile decrypts and re-encodes one fragment at a time and
// feeds the resulting fragmented MP4 directly to ffmpeg over stdin. The caller
// supplies either the high-mode encrypted whole-track cache or low-mode raw
// checkpoint; both retain only the current encrypted and decrypted fragments
// in Go's heap. No dec-* or fix-*/in.m4a copy is made.
func (p *MP4Processor) streamDecryptToFlatFile(
	ctx context.Context,
	r io.Reader,
	outPath string,
	keys []string,
	decrypt func(key string, samples [][]byte) ([][]byte, error),
	onProgress func(consumed uint64),
	afterInput func(),
) error {
	return p.produceToFlatFile(ctx, outPath, func(w io.Writer) error {
		return p.streamDecryptToWriter(ctx, r, w, keys, decrypt, onProgress)
	}, afterInput)
}

// checkIntegrityFile decodes the file at path completely with ffmpeg and
// returns nil only when ffmpeg exits cleanly and reports nothing. The error
// carries ffmpeg's own diagnostics, capped at maxToolDiagnosticBytes, because
// it becomes the item's error and the only clue to what was wrong.
func (p *MP4Processor) checkIntegrityFile(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, p.cfg.Tools.FFmpeg, "-y", "-v", "error", "-i", path, "-c:a", "pcm_s16le", "-f", "null", "/dev/null")
	out, err := cmd.CombinedOutput()
	diagnostic := toolDiagnostic(out)
	switch {
	case err != nil && diagnostic != "":
		return fmt.Errorf("ffmpeg decode: %w: %s", err, diagnostic)
	case err != nil:
		return fmt.Errorf("ffmpeg decode: %w", err)
	case diagnostic != "":
		return fmt.Errorf("ffmpeg decode reported errors: %s", diagnostic)
	}
	return nil
}

// maxToolDiagnosticBytes bounds the ffmpeg output kept in an error. A corrupt
// track can make ffmpeg print one line per damaged packet; the first lines say
// what is wrong and the item's error must stay readable.
const maxToolDiagnosticBytes = 1024

func toolDiagnostic(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) <= maxToolDiagnosticBytes {
		return text
	}
	return fmt.Sprintf("%s ... (%d more bytes)", trimToRuneBoundary(text, maxToolDiagnosticBytes), len(text)-maxToolDiagnosticBytes)
}

// writeMetadata writes the track's tags, lyrics and (when embed_cover is on)
// cover art into the MP4 at path.
func (p *MP4Processor) writeMetadata(ctx context.Context, path string, song applemusic.Song, lyrics string, cover []byte) error {
	tags := &mp4tag.MP4Tags{
		Title:       song.Name,
		Artist:      song.ArtistName,
		Album:       song.AlbumName,
		AlbumArtist: firstNonEmpty(song.AlbumArtist, song.ArtistName),
		Composer:    song.ComposerName,
		CustomGenre: firstGenre(song.GenreNames),
		Lyrics:      lyrics,
		TrackNumber: int16(song.TrackNumber),
		TrackTotal:  int16(song.TrackCount),
		DiscNumber:  int16(song.DiscNumber),
		DiscTotal:   int16(song.DiscCount),
		Date:        firstNonEmpty(song.AlbumRelease, song.ReleaseDate),
		Copyright:   song.Copyright,
		Publisher:   song.RecordLabel,
		Custom: map[string]string{
			"PERFORMER":   song.ArtistName,
			"RELEASETIME": song.ReleaseDate,
			"ISRC":        song.ISRC,
			"LABEL":       song.RecordLabel,
			"UPC":         song.UPC,
		},
	}
	tags.TitleSort = song.Name
	tags.ArtistSort = song.ArtistName
	tags.AlbumSort = song.AlbumName
	tags.AlbumArtistSort = firstNonEmpty(song.AlbumArtist, song.ArtistName)
	tags.ComposerSort = song.ComposerName
	if song.ContentRating == "explicit" {
		tags.ItunesAdvisory = mp4tag.ItunesAdvisoryExplicit
	} else if song.ContentRating == "clean" {
		tags.ItunesAdvisory = mp4tag.ItunesAdvisoryClean
	} else {
		tags.ItunesAdvisory = mp4tag.ItunesAdvisoryNone
	}
	tags.ItunesAlbumID = itunesTagID(ctx, "album_id", song.AlbumID)
	tags.ItunesArtistID = itunesTagID(ctx, "artist_id", song.ArtistID)
	if p.cfg.Download.EmbedCover && len(cover) > 0 {
		tags.Pictures = []*mp4tag.MP4Picture{{Format: mp4tag.ImageTypeAuto, Data: cover}}
	}
	return writeMP4Tags(path, tags)
}

// readInitSegment reads top-level boxes through the moov box that completes the
// initialization segment, tracking the running byte offset so subsequent
// fragment boxes report the correct absolute positions (required for mdat
// sample extraction).
func readInitSegment(r io.Reader, offset uint64) (*mp4.InitSegment, uint64, error) {
	init := mp4.NewMP4Init()
	for {
		box, err := mp4.DecodeBox(offset, r)
		if err != nil {
			return nil, offset, err
		}
		boxType := box.Type()
		if init.Ftyp == nil && boxType != "ftyp" {
			return nil, offset, fmt.Errorf("unexpected box type %s, expected ftyp", boxType)
		}
		init.AddChild(box)
		offset += box.Size()
		if boxType == "moov" {
			return init, offset, nil
		}
	}
}

// readNextFragment reads the next moof+mdat fragment. It returns (nil, offset,
// nil) at end of stream.
func readNextFragment(r io.Reader, offset uint64) (*mp4.Fragment, uint64, error) {
	frag := mp4.NewFragment()
	for {
		box, err := mp4.DecodeBox(offset, r)
		if errors.Is(err, io.EOF) {
			return nil, offset, nil
		}
		if err != nil {
			return nil, offset, err
		}
		boxType := box.Type()
		offset += box.Size()
		switch boxType {
		case "moof", "emsg", "prft":
			frag.AddChild(box)
		case "mdat":
			frag.AddChild(box)
			if frag.Moof == nil {
				return nil, offset, fmt.Errorf("mdat box without preceding moof (ends @ offset %d)", offset)
			}
			return frag, offset, nil
		default:
			// Ignore unexpected top-level boxes between fragments.
		}
	}
}

// filterEncryptionSampleGroups removes sbgp/sgpd boxes that describe encryption
// key assignment (grouping type seig/seam), leaving other sample groups (e.g.
// roll) untouched.
func filterEncryptionSampleGroups(children []mp4.Box) []mp4.Box {
	out := make([]mp4.Box, 0, len(children))
	for _, child := range children {
		switch box := child.(type) {
		case *mp4.SbgpBox:
			if box.GroupingType == "seig" || box.GroupingType == "seam" {
				continue
			}
		case *mp4.SgpdBox:
			if box.GroupingType == "seig" || box.GroupingType == "seam" {
				continue
			}
		}
		out = append(out, child)
	}
	return out
}

// collapseSampleEntries reduces a sample description table that holds multiple
// (now identical, post-decryption) entries down to a single entry. Apple Music
// ships two entries because two IVs are used to encrypt the track.
func collapseSampleEntries(init *mp4.InitSegment) {
	for _, trak := range init.Moov.Traks {
		if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil {
			continue
		}
		stsd := trak.Mdia.Minf.Stbl.Stsd
		if stsd == nil || len(stsd.Children) <= 1 {
			continue
		}
		stsd.Children = stsd.Children[:1]
		stsd.SampleCount = 1
		if init.Moov.Mvex != nil && trak.Tkhd != nil {
			for _, trex := range init.Moov.Mvex.Trexs {
				if trex.TrackID == trak.Tkhd.TrackID {
					trex.DefaultSampleDescriptionIndex = 1
				}
			}
		}
	}
}

// itunesTagID converts a catalog ID for go-mp4tag's int32 plID/atID fields.
// Catalog IDs are approaching 2^31-1; an ID that no longer fits is left out of
// the tags, which is logged at debug level so the omission is explainable.
func itunesTagID(ctx context.Context, field, value string) int32 {
	if value == "" {
		return 0
	}
	id, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		logging.FromContext(ctx, nil).Debug("catalog id does not fit the mp4 tag; omitted", "field", field, "value", value, "error", err)
		return 0
	}
	return int32(id)
}

func firstGenre(genres []string) string {
	if len(genres) == 0 {
		return ""
	}
	return genres[0]
}
