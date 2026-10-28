package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

type selectedDownloadMedia struct {
	info selectedMediaInfo
	// raw holds the still-encrypted whole track only in high-memory mode. Keeping
	// one immutable copy allows decrypt retries without another CDN request while
	// avoiding the raw-* scratch file and its write/read round trip.
	raw []byte
	// rawPath is the on-disk location of the still-encrypted media downloaded by
	// downloadSelectedEnhancedMedia in low-memory mode. It is kept on disk so a
	// whole Hi-Res track's encrypted bytes aren't pinned across the decrypt phase,
	// and so a decrypt-phase retry can re-read them without re-fetching.
	rawPath string
	// releaseInFlight owns the global media-backpressure permit acquired just
	// before rawPath was downloaded. It stays held while the media waits for
	// decryption and is idempotent so cleanup paths can safely converge.
	releaseInFlight func()
}

func (d *Downloader) selectEnhancedMedia(ctx context.Context, job domain.Job, item *domain.JobItem, song applemusic.Song, codec string, reporter jobs.Reporter, set publishStage) (selectedDownloadMedia, error) {
	set(domain.ItemResolving, fmt.Sprintf("Selecting %s media stream", strings.ToUpper(codec)), nil)
	info, err := d.selectEnhancedStream(ctx, job.Storefront, song, codec)
	if err != nil {
		return selectedDownloadMedia{}, err
	}
	d.setItemQuality(ctx, reporter, item, info.BitDepth, info.SampleRate, info.Bandwidth)
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "codec_selected", Phase: codec, Payload: domain.MarshalEventPayload(*item, map[string]any{
		"codec_id": info.CodecID, "bit_depth": info.BitDepth, "sample_rate": info.SampleRate, "bitrate": info.Bandwidth,
		"attempt": item.Attempt, "max_attempts": item.MaxAttempts,
	})})

	return selectedDownloadMedia{info: info}, nil
}

// selectEnhancedStream is the download boundary between media discovery and
// transfer. Quality inventory shares its source resolver and variant selector,
// but stops at the master playlist instead of validating a concrete media
// playlist here.
func (d *Downloader) selectEnhancedStream(ctx context.Context, storefront string, song applemusic.Song, codec string) (selectedMediaInfo, error) {
	master, err := d.resolveEnhancedHLS(ctx, storefront, song)
	if err != nil {
		return selectedMediaInfo{}, err
	}
	info, err := extractMedia(ctx, d.http, master, codec, d.cfg.Download.ALACMaxSampleRate, d.cfg.Download.ALACMaxBitDepth, d.requestGate)
	if err != nil {
		return selectedMediaInfo{}, fmt.Errorf("select %s media: %w", codec, err)
	}
	return info, nil
}

// resolveEnhancedHLS chooses the master-playlist source shared by downloads
// and quality inventory. The caller decides whether to continue through a
// concrete media playlist (download) or only inspect the master variants
// (quality query).
func (d *Downloader) resolveEnhancedHLS(ctx context.Context, storefront string, song applemusic.Song) (string, error) {
	master := song.EnhancedHLS
	if d.cfg.Catalog.DeveloperTokenSigningEnabled() {
		// A self-signed developer token cannot read enhancedHls, so the master
		// playlist comes from either the wrapper's authorized device manifest
		// or a scraped web-player token, per catalog.signed_mode_hls_source.
		if d.cfg.Catalog.EnhancedHLSFromWebToken() {
			hls, err := d.catalog.EnhancedHLSViaWebToken(ctx, storefront, song.ID)
			if err != nil {
				return "", fmt.Errorf("fetch enhanced hls via web token: %w", err)
			}
			master = hls
		} else {
			m3u8, err := d.wrapper.M3U8(ctx, song.ID)
			if err != nil {
				return "", fmt.Errorf("request device m3u8: %w", err)
			}
			master = m3u8
		}
	}
	if master == "" {
		return "", fmt.Errorf("no enhanced hls manifest")
	}
	return master, nil
}

func (d *Downloader) downloadSelectedEnhancedMedia(ctx context.Context, selected selectedDownloadMedia, codec, jobID, outPath string, set publishStage) (selectedDownloadMedia, error) {
	d.ensureMediaLimits()
	codecName := strings.ToUpper(codec)
	onProgress := func(p float64) {
		if p < 0 {
			return // Content-Length unknown; the meter stays at 0
		}
		set(domain.ItemDownloading, fmt.Sprintf("%s download %.0f%%", codecName, p*100), downloadFrac(p))
	}
	set(domain.ItemWaitingDownload, "waiting for download slot", nil)
	releaseInFlight, err := d.inFlightLimit.Acquire(ctx)
	if err != nil {
		return selectedDownloadMedia{}, err
	}
	releaseDownload, err := d.downloadLimit.Acquire(ctx)
	if err != nil {
		releaseInFlight()
		return selectedDownloadMedia{}, err
	}
	set(domain.ItemDownloading, fmt.Sprintf("Downloading %s encrypted media", codecName), startDownload)
	if d.cfg.Download.MemoryMode == config.MemoryModeHigh {
		// High-memory mode keeps exactly one whole-track encrypted copy. The
		// fragment decrypt/remux stage remains streaming, so parsed, plaintext,
		// and remuxed whole-track copies never accumulate beside it. The
		// in-flight permit stays held until decrypt has consumed the bytes. A
		// validated Range reconnect may repair one interrupted response without
		// creating a persistent checkpoint.
		raw, err := func() ([]byte, error) {
			defer releaseDownload()
			return downloadBytesWithRangeResume(ctx, d.http, selected.info.MediaURI, onProgress)
		}()
		if err != nil {
			releaseInFlight()
			return selectedDownloadMedia{}, fmt.Errorf("download encrypted media: %w", err)
		}
		selected.raw = raw
		selected.releaseInFlight = releaseInFlight
		return selected, nil
	}

	// Low-memory mode streams the encrypted response to a resumable checkpoint
	// and reads it back fragment-by-fragment during decrypt. The encrypted bytes
	// stay on disk, so they never occupy a full-track []byte in memory.
	rawPath, err := func() (string, error) {
		defer releaseDownload()
		return downloadToFile(ctx, d.http, selected.info.MediaURI, d.cfg.Download.TempDir, jobID, outPath, onProgress)
	}()
	if err != nil {
		releaseInFlight()
		return selectedDownloadMedia{}, fmt.Errorf("download encrypted media: %w", err)
	}
	selected.rawPath = rawPath
	selected.releaseInFlight = releaseInFlight
	return selected, nil
}

func (d *Downloader) downloadEnhancedCodec(ctx context.Context, job domain.Job, item *domain.JobItem, song applemusic.Song, codec, lyrics string, cover []byte, outPath string, selected selectedDownloadMedia, reporter jobs.Reporter, set publishStage) error {
	d.ensureMediaLimits()
	info := selected.info
	var (
		rawReader io.Reader
		rawSize   int64
		rawFile   *os.File
		err       error
	)
	if d.cfg.Download.MemoryMode == config.MemoryModeHigh {
		rawReader = bytes.NewReader(selected.raw)
		rawSize = int64(len(selected.raw))
	} else {
		rawFile, err = os.Open(selected.rawPath)
		if err != nil {
			return fmt.Errorf("open downloaded media: %w", err)
		}
		defer rawFile.Close()
		rawReader = rawFile
		if fi, statErr := rawFile.Stat(); statErr == nil {
			rawSize = fi.Size()
		}
	}

	// Both modes decrypt one fragment at a time and pipe the resulting plaintext
	// fragmented MP4 straight into ffmpeg. Only their encrypted input backing
	// differs: low mode reads the resumable raw checkpoint while high mode reads
	// its in-memory copy. The flattened, verified and tagged output is staged
	// here before the atomic final move.
	flatFile, err := os.CreateTemp(d.cfg.Download.TempDir, "flat-*.m4a")
	if err != nil {
		return fmt.Errorf("create flatten output: %w", err)
	}
	flatPath := flatFile.Name()
	flatFile.Close()
	defer os.Remove(flatPath)

	set(domain.ItemWaitingDecrypt, "waiting for decrypt slot", nil)
	releaseDecrypt, err := d.decryptLimit.Acquire(ctx)
	if err != nil {
		return err
	}
	set(domain.ItemDecrypting, "opening decrypt session", startDecrypt)
	streamErr, closeErr := func() (error, error) {
		defer releaseDecrypt()
		session, openErr := d.wrapper.NewDecryptSession(ctx, song.ID)
		if openErr != nil {
			return fmt.Errorf("open decrypt session: %w", openErr), nil
		}
		set(domain.ItemDecrypting, "decrypting", nil)
		// The decrypt meter tracks encrypted bytes consumed; the total sample
		// count isn't known until the last fragment is read.
		decryptFragment := func(key string, samples [][]byte) ([][]byte, error) {
			return session.DecryptFragment(key, samples)
		}
		onProgress := func(consumed uint64) {
			if rawSize > 0 {
				p := float64(consumed) / float64(rawSize)
				set(domain.ItemDecrypting, fmt.Sprintf("decrypting %.0f%%", clampFrac(p)*100), decryptFrac(p))
			}
		}
		// mp4ff parses one fragment, the wrapper returns one plaintext fragment,
		// and DataParts writes those samples directly into ffmpeg's stdin without
		// another concatenation or a whole-track dec-* intermediate.
		return d.mp4.streamDecryptToFlatFile(ctx, rawReader, flatPath, info.Keys, decryptFragment, onProgress, func() {
			// The decrypted input has reached EOF and ffmpeg is now flushing the
			// progressive MP4 trailer. Publish remuxing inside that real window,
			// before produceToFlatFile waits for ffmpeg to exit. Decrypt is done
			// by definition here, whether or not the meter got to say so.
			set(domain.ItemRemuxing, "remuxing", decryptFrac(1))
		}), session.Close()
	}()
	if streamErr != nil {
		return fmt.Errorf("decrypt and flatten media: %w", streamErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close decrypt session: %w", closeErr)
	}
	set(domain.ItemRemuxing, "remuxed", markRemuxed)
	if selected.releaseInFlight != nil {
		selected.releaseInFlight()
	}

	// Flatten the decrypted fragmented MP4 into a regular progressive MP4 (also
	// normalises the ftyp brand) on temp storage, then verify and tag it there,
	// and only move the finished file to its final path. Keeping the flatten
	// write, the integrity read-back, and the tag rewrite on temp (typically
	// fast local disk) means a possibly-slow downloads volume sees just one
	// sequential write; see finalizeToOutput. The decoder configuration is
	// carried over from the original init segment, so no esds fixup is needed.
	set(domain.ItemSaving, "saving", nil)
	if d.cfg.Download.CheckIntegrity {
		if !d.mp4.checkIntegrityFile(ctx, flatPath) {
			if codec != "alac" {
				return fmt.Errorf("integrity check failed")
			}
			// repairALACFile re-verifies after patching, so reaching the mark
			// below still means the file passed a real check.
			if err := d.repairALACFile(ctx, job, item, flatPath, codec, reporter); err != nil {
				return err
			}
		}
		set(domain.ItemSaving, "integrity verified", markVerified)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if d.cfg.Download.SaveLyricsFile && lyrics != "" {
		ext := ".lrc"
		if d.cfg.Download.LyricsFormat == "ttml" {
			ext = ".ttml"
		}
		if err := os.WriteFile(strings.TrimSuffix(outPath, ".m4a")+ext, []byte(lyrics), 0o644); err != nil {
			return fmt.Errorf("write lyrics file: %w", err)
		}
	}
	set(domain.ItemTagging, "writing metadata", nil)
	if err := d.mp4.writeMetadata(ctx, flatPath, song, lyrics, cover, songInfo{Codec: codec}); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	set(domain.ItemTagging, "metadata written", markTagged)
	// Only ever expose a complete, tagged file at outPath: finalizeToOutput
	// renames (same filesystem) or copies through a .part then renames
	// (cross-filesystem), so handleExistingOutput's "bare existence = done" skip
	// stays correct even if the process dies mid-move.
	if err := finalizeToOutput(flatPath, outPath); err != nil {
		return fmt.Errorf("finalize output file: %w", err)
	}
	item.Status = domain.ItemCompleted
	markSaved(&item.Progress)
	item.Progress.CompleteTransfers()
	item.OutputPath = outPath
	item.Codec = codec
	item.FileSize = outputFileSize(outPath)
	return nil
}

// repairALACFile is the ALAC integrity-repair fallback, run only when the
// flattened file fails the decode check. It reads the file back, patches
// malformed ALAC packet terminators, re-verifies, and writes the repaired bytes
// in place — keeping the whole-file read off the common (already-valid) path.
func (d *Downloader) repairALACFile(ctx context.Context, job domain.Job, item *domain.JobItem, path, codec string, reporter jobs.Reporter) error {
	outBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("integrity check failed; read for alac repair: %w", err)
	}
	repaired, patched, err := repairALACTerminators(outBytes)
	if err != nil {
		return fmt.Errorf("integrity check failed; alac repair failed: %w", err)
	}
	if patched == 0 {
		return fmt.Errorf("integrity check failed; alac repair found no malformed packets")
	}
	payload, _ := json.Marshal(map[string]any{"patched_packets": patched})
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: "alac_repaired", Phase: codec, Payload: string(payload)})
	if err := os.WriteFile(path, repaired, 0o644); err != nil {
		return fmt.Errorf("integrity check failed; write repaired alac: %w", err)
	}
	if !d.mp4.checkIntegrityFile(ctx, path) {
		return fmt.Errorf("integrity check failed after alac repair")
	}
	return nil
}
