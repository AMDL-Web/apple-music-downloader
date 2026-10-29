package media

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/logging"
)

// selectedDownloadMedia is one enhanced codec's chosen stream and, after the
// fetch phase, its encrypted media.
type selectedDownloadMedia struct {
	info selectedMediaInfo
	// estimatedBytes is the size info's bandwidth predicts for the track.
	estimatedBytes int64
	encryptedMedia
}

func (d *Downloader) selectEnhancedMedia(ctx context.Context, job domain.Job, item *domain.JobItem, song applemusic.Song, codec string, reporter jobs.Reporter, set publishStage) (selectedDownloadMedia, error) {
	set(domain.ItemResolving, fmt.Sprintf("Selecting %s media stream", strings.ToUpper(codec)), nil)
	info, err := d.selectEnhancedStream(ctx, job.Storefront, song, codec)
	if err != nil {
		return selectedDownloadMedia{}, err
	}
	d.setItemQuality(ctx, reporter, item, info.BitDepth, info.SampleRate, info.Bandwidth)
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventCodecSelected, Phase: codec, Payload: domain.MarshalEventPayload(*item, map[string]any{
		"codec_id": info.CodecID, "bit_depth": info.BitDepth, "sample_rate": info.SampleRate, "bitrate": info.Bandwidth,
		"attempt": item.Attempt, "max_attempts": item.MaxAttempts,
	})})

	return selectedDownloadMedia{info: info, estimatedBytes: estimatedMediaBytes(song.DurationInMillis, info.Bandwidth)}, nil
}

// selectEnhancedStream is the download boundary between media discovery and
// transfer. Quality inventory shares its source resolver and variant selector,
// but stops at the master playlist instead of validating a concrete media
// playlist here. It runs for real in simulate mode too.
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
// (quality query). A song without a manifest wraps
// applemusic.ErrNoEnhancedHLS, which is not retryable, so the codec loop falls
// back at once instead of spending its retry budget.
//
// The web player's enhancedHls master can leave out a hi-res-lossless track's
// higher ALAC renditions that the wrapper's device manifest lists (88.2 and
// 96 kHz have been seen missing), and nothing in it says when. So in legacy
// mode such a track takes the device manifest, and keeps enhancedHls only if
// the wrapper cannot supply one.
func (d *Downloader) resolveEnhancedHLS(ctx context.Context, storefront string, song applemusic.Song) (string, error) {
	master := song.EnhancedHLS
	switch {
	case d.cfg.Catalog.DeveloperTokenSigningEnabled():
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
	case song.HiResLossless:
		m3u8, err := d.wrapper.M3U8(ctx, song.ID)
		if err == nil && m3u8 == "" {
			err = errors.New("empty manifest")
		}
		if err == nil {
			master = m3u8
			break
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("request device m3u8: %w", err)
		}
		logging.FromContext(ctx, d.logger).Warn("device manifest unavailable; using the web manifest, which may lack hi-res renditions",
			"song_id", song.ID, "error", err)
	}
	if master == "" {
		return "", fmt.Errorf("song %s: %w", song.ID, applemusic.ErrNoEnhancedHLS)
	}
	return master, nil
}

// downloadSelectedEnhancedMedia is the enhanced codec's fetch phase: it waits
// for the in-flight and download permits and transfers the still-encrypted
// media. The in-flight permit is returned held, in selected.releaseInFlight,
// until decryption has consumed the bytes.
func (d *Downloader) downloadSelectedEnhancedMedia(ctx context.Context, selected selectedDownloadMedia, codec, jobID, outPath string, set publishStage) (selectedDownloadMedia, error) {
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
	media, err := func() (encryptedMedia, error) {
		defer releaseDownload()
		return d.backend().fetch(ctx, fetchRequest{
			codec: codec, uri: selected.info.MediaURI, jobID: jobID, outPath: outPath,
			estimatedBytes: selected.estimatedBytes,
		}, onProgress)
	}()
	if err != nil {
		releaseInFlight()
		return selectedDownloadMedia{}, fmt.Errorf("download encrypted media: %w", err)
	}
	media.releaseInFlight = releaseInFlight
	selected.encryptedMedia = media
	return selected, nil
}

// downloadEnhancedCodec is the enhanced codec's decrypt phase: it decrypts the
// fetched media one fragment at a time straight into ffmpeg, then verifies,
// tags and commits the result (finalizeTrackOutput). Both memory modes stream;
// only the encrypted input's backing differs.
func (d *Downloader) downloadEnhancedCodec(ctx context.Context, job domain.Job, item *domain.JobItem, song applemusic.Song, codec, lyrics string, cover []byte, outPath string, selected selectedDownloadMedia, reporter jobs.Reporter, set publishStage) error {
	backend := d.backend()
	input, inputSize, closeInput, err := selected.open()
	if err != nil {
		return fmt.Errorf("open downloaded media: %w", err)
	}
	defer closeInput()

	staged, cleanupStaged, err := backend.stage(selected.encryptedMedia)
	if err != nil {
		return fmt.Errorf("create flatten output: %w", err)
	}
	defer cleanupStaged()

	set(domain.ItemWaitingDecrypt, "waiting for decrypt slot", nil)
	releaseDecrypt, err := d.decryptLimit.Acquire(ctx)
	if err != nil {
		return err
	}
	set(domain.ItemDecrypting, "opening decrypt session", startDecrypt)
	err = func() error {
		defer releaseDecrypt()
		return backend.decrypt(ctx, decryptRequest{
			codec: codec, songID: song.ID, input: input, size: inputSize, staged: staged, keys: selected.info.Keys,
		}, decryptHooks{
			ready: func() { set(domain.ItemDecrypting, "decrypting", nil) },
			// The decrypt meter tracks encrypted bytes consumed; the total
			// sample count isn't known until the last fragment is read.
			progress: func(p float64) {
				set(domain.ItemDecrypting, fmt.Sprintf("decrypting %.0f%%", clampFrac(p)*100), decryptFrac(p))
			},
			// The decrypted input has reached EOF and ffmpeg is now flushing the
			// progressive MP4 trailer. Publish remuxing inside that real window.
			// Decrypt is done by definition here, whether or not the meter got
			// to say so.
			afterInput: func() { set(domain.ItemRemuxing, "remuxing", decryptFrac(1)) },
		})
	}()
	if err != nil {
		return err
	}
	set(domain.ItemRemuxing, "remuxed", markRemuxed)
	selected.release()

	return d.finalizeTrackOutput(ctx, backend, trackOutput{
		job: job, item: item, reporter: reporter, song: song, codec: codec,
		lyrics: lyrics, cover: cover, staged: staged, outPath: outPath,
		labels: enhancedOutputLabels, allowALACRepair: codec == "alac",
	}, set)
}
