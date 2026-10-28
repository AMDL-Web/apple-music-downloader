package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/limits"

	widevine "github.com/iyear/gowidevine"
	wvpb "github.com/iyear/gowidevine/widevinepb"
	"google.golang.org/protobuf/proto"
)

type aacLCMedia struct {
	MediaURI string
	KeyURI   string
	KID      string
}

func extractAACLCMedia(ctx context.Context, client *http.Client, playlistURL string, gates ...*limits.RequestGate) (aacLCMedia, error) {
	body, err := downloadText(ctx, client, playlistURL, gates...)
	if err != nil {
		return aacLCMedia{}, err
	}
	var media aacLCMedia
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			keyURI := parseAttrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))["URI"]
			if keyURI != "" {
				media.KeyURI = keyURI
				parts := strings.SplitN(keyURI, ",", 2)
				if len(parts) == 2 {
					media.KID = parts[1]
				}
			}
		}
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			mapURI := parseAttrs(strings.TrimPrefix(line, "#EXT-X-MAP:"))["URI"]
			if mapURI != "" {
				media.MediaURI = absURL(playlistURL, mapURI)
			}
		}
	}
	if media.MediaURI == "" {
		return aacLCMedia{}, fmt.Errorf("aac-lc playlist has no EXT-X-MAP media URI")
	}
	if media.KeyURI == "" || media.KID == "" {
		return aacLCMedia{}, fmt.Errorf("aac-lc playlist has no Widevine key URI")
	}
	if _, err := base64.StdEncoding.DecodeString(media.KID); err != nil {
		return aacLCMedia{}, fmt.Errorf("decode aac-lc KID: %w", err)
	}
	return media, nil
}

func newWidevineSession(kidBase64 string) ([]byte, func([]byte) ([]*widevine.Key, error), error) {
	deviceRaw, err := base64.StdEncoding.DecodeString(widevineDeviceBase64)
	if err != nil {
		return nil, nil, fmt.Errorf("decode embedded Widevine device: %w", err)
	}
	device, err := widevine.NewDevice(widevine.FromWVD(bytes.NewReader(deviceRaw)))
	if err != nil {
		return nil, nil, fmt.Errorf("load Widevine device: %w", err)
	}
	pssh, err := makeWidevinePSSH(kidBase64)
	if err != nil {
		return nil, nil, err
	}
	challenge, parseLicense, err := widevine.NewCDM(device).GetLicenseChallenge(pssh, wvpb.LicenseType_AUTOMATIC, false)
	if err != nil {
		return nil, nil, fmt.Errorf("generate Widevine license challenge: %w", err)
	}
	return challenge, parseLicense, nil
}

func makeWidevinePSSH(kidBase64 string) (*widevine.PSSH, error) {
	kid, err := base64.StdEncoding.DecodeString(kidBase64)
	if err != nil {
		return nil, fmt.Errorf("decode Widevine KID: %w", err)
	}
	data, err := proto.Marshal(&wvpb.WidevinePsshData{KeyIds: [][]byte{kid}})
	if err != nil {
		return nil, fmt.Errorf("marshal Widevine PSSH data: %w", err)
	}
	systemID := []byte{0xed, 0xef, 0x8b, 0xa9, 0x79, 0xd6, 0x4a, 0xce, 0xa3, 0xc8, 0x27, 0xdc, 0xd5, 0x1d, 0x21, 0xed}
	var box bytes.Buffer
	_ = binary.Write(&box, binary.BigEndian, uint32(32+len(data)))
	box.WriteString("pssh")
	_ = binary.Write(&box, binary.BigEndian, uint32(0))
	box.Write(systemID)
	_ = binary.Write(&box, binary.BigEndian, uint32(len(data)))
	box.Write(data)
	pssh, err := widevine.NewPSSH(box.Bytes())
	if err != nil {
		return nil, fmt.Errorf("parse generated Widevine PSSH: %w", err)
	}
	return pssh, nil
}

func decodeWidevineLicense(value string) ([]byte, error) {
	license, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode Widevine license: %w", err)
	}
	return license, nil
}

func parseWidevineLicenseKeys(licenseValue string, parseLicense func([]byte) ([]*widevine.Key, error)) ([]*widevine.Key, error) {
	license, err := decodeWidevineLicense(licenseValue)
	if err != nil {
		return nil, err
	}
	keys, err := parseLicense(license)
	if err != nil {
		return nil, fmt.Errorf("parse Widevine license: %w", err)
	}
	return keys, nil
}

// decryptWidevineMP4ToWriter writes gowidevine's decrypted fragmented MP4
// directly to its consumer. In production that consumer is ffmpeg's stdin, so
// AAC-LC no longer materializes the additional whole-track output []byte or a
// fix-*/in.m4a file beside the still-encrypted retry buffer. Gowidevine v0.1.3
// still decodes the input into an in-memory MP4 box tree before it writes it.
func decryptWidevineMP4ToWriter(raw io.Reader, keys []*widevine.Key, dst io.Writer) error {
	if err := widevine.DecryptMP4Auto(raw, keys, dst); err != nil {
		return fmt.Errorf("decrypt AAC-LC MP4: %w", err)
	}
	return nil
}

func (p *MP4Processor) decryptWidevineToFlatFile(
	ctx context.Context,
	raw io.Reader,
	keys []*widevine.Key,
	outPath string,
	afterInput func(),
) error {
	return p.produceToFlatFile(ctx, outPath, func(dst io.Writer) error {
		return decryptWidevineMP4ToWriter(raw, keys, dst)
	}, afterInput)
}

// fetchAACLCMedia resolves the AAC-LC media playlist and downloads the
// still-encrypted stream into memory. Kept separate from decryptAACLC so a
// decrypt-phase retry can reuse these bytes instead of re-hitting the CDN.
func (d *Downloader) fetchAACLCMedia(ctx context.Context, job domain.Job, item *domain.JobItem, song applemusic.Song, reporter jobs.Reporter, set publishStage) (aacLCMedia, []byte, func(), error) {
	d.ensureMediaLimits()
	set(domain.ItemResolving, "requesting AAC-LC WebPlayback asset", nil)
	playlistURL, err := d.wrapper.WebPlayback(ctx, song.ID)
	if err != nil {
		return aacLCMedia{}, nil, nil, fmt.Errorf("request AAC-LC WebPlayback: %w", err)
	}
	media, err := extractAACLCMedia(ctx, d.http, playlistURL, d.requestGate)
	if err != nil {
		return aacLCMedia{}, nil, nil, fmt.Errorf("parse AAC-LC media playlist: %w", err)
	}
	_ = reporter.Event(ctx, domain.Event{JobID: job.ID, ItemID: item.ID, Type: domain.EventCodecSelected, Phase: "aac-lc", Payload: domain.MarshalEventPayload(*item, map[string]any{
		"codec_id": "aac-lc", "attempt": item.Attempt, "max_attempts": item.MaxAttempts,
	})})

	set(domain.ItemWaitingDownload, "waiting for download slot", nil)
	releaseInFlight, err := d.inFlightLimit.Acquire(ctx)
	if err != nil {
		return aacLCMedia{}, nil, nil, err
	}
	releaseDownload, err := d.downloadLimit.Acquire(ctx)
	if err != nil {
		releaseInFlight()
		return aacLCMedia{}, nil, nil, err
	}
	set(domain.ItemDownloading, "downloading encrypted AAC-LC media", startDownload)
	// Stream-download, advancing the download meter per chunk.
	raw, err := func() ([]byte, error) {
		defer releaseDownload()
		return downloadBytes(ctx, d.http, media.MediaURI, func(p float64) {
			if p >= 0 {
				set(domain.ItemDownloading, fmt.Sprintf("downloading %.0f%%", p*100), downloadFrac(p))
			}
		})
	}()
	if err != nil {
		releaseInFlight()
		return aacLCMedia{}, nil, nil, fmt.Errorf("download encrypted AAC-LC media: %w", err)
	}
	return media, raw, releaseInFlight, nil
}

// decryptAACLC takes the already-downloaded encrypted bytes from
// fetchAACLCMedia and acquires the license, decrypts, and writes the final
// file. On retry this re-runs without downloading media again. Gowidevine's
// decoded box tree is written straight to ffmpeg, avoiding the previous extra
// whole-track output buffer and fix-*/in.m4a intermediate. Gowidevine itself
// still retains the decoded media boxes until it has emitted the track.
func (d *Downloader) decryptAACLC(ctx context.Context, item *domain.JobItem, song applemusic.Song, media aacLCMedia, raw []byte, lyrics string, cover []byte, outPath string, releaseInFlight func(), set publishStage) error {
	d.ensureMediaLimits()
	// Flatten, verify, and tag on temp storage, then move the finished file to
	// its final path, same as downloadEnhancedCodec (see finalizeToOutput).
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
	set(domain.ItemDecrypting, "acquiring Widevine license", startDecrypt)
	// afterInput is the successful producer boundary, while this defer covers
	// license, producer, stdin, ffmpeg-start, and cancellation failures. Keep a
	// local once even though Semaphore's release is itself idempotent: ownership
	// is explicit here and future limiter implementations cannot over-release.
	var releaseDecryptOnce sync.Once
	releaseDecryptPermit := func() { releaseDecryptOnce.Do(releaseDecrypt) }
	defer releaseDecryptPermit()
	challenge, parseLicense, err := newWidevineSession(media.KID)
	if err != nil {
		return err
	}
	license, err := d.wrapper.License(ctx, song.ID, base64.StdEncoding.EncodeToString(challenge), media.KeyURI)
	if err != nil {
		return fmt.Errorf("request AAC-LC license: %w", err)
	}
	keys, err := parseWidevineLicenseKeys(license, parseLicense)
	if err != nil {
		return err
	}
	// AAC-LC decrypts in one gowidevine pass with no intermediate byte count to
	// report, so the decrypt meter jumps straight from 0 to 1 at the remux
	// boundary below rather than sweeping.
	set(domain.ItemDecrypting, "decrypting AAC-LC", nil)
	if err := d.mp4.decryptWidevineToFlatFile(ctx, bytes.NewReader(raw), keys, flatPath, func() {
		// Decryption has consumed the immutable retry buffer. Match the old
		// phase boundary: release both permits before the container's final
		// flush and publish remuxing until cmd.Wait completes.
		releaseDecryptPermit()
		if releaseInFlight != nil {
			releaseInFlight()
		}
		set(domain.ItemRemuxing, "remuxing AAC-LC", decryptFrac(1))
	}); err != nil {
		return fmt.Errorf("normalize AAC-LC container: %w", err)
	}
	set(domain.ItemRemuxing, "AAC-LC remuxed", markRemuxed)

	set(domain.ItemSaving, "saving AAC-LC", nil)
	// The ffmpeg copy pass also normalises the M4A brand and creates the
	// moov.udta.meta.ilst structure that go-mp4tag writes into. The decrypted
	// WebPlayback asset has no udta box, so tagging would otherwise fail.
	if d.cfg.Download.CheckIntegrity {
		if !d.mp4.checkIntegrityFile(ctx, flatPath) {
			return fmt.Errorf("AAC-LC integrity check failed")
		}
		set(domain.ItemSaving, "AAC-LC integrity verified", markVerified)
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
	set(domain.ItemTagging, "writing AAC-LC metadata", nil)
	if err := d.mp4.writeMetadata(ctx, flatPath, song, lyrics, cover, songInfo{Codec: "aac-lc"}); err != nil {
		return fmt.Errorf("write AAC-LC metadata: %w", err)
	}
	set(domain.ItemTagging, "AAC-LC metadata written", markTagged)
	if err := finalizeToOutput(flatPath, outPath); err != nil {
		return fmt.Errorf("finalize AAC-LC output file: %w", err)
	}
	item.Status = domain.ItemCompleted
	markSaved(&item.Progress)
	item.Progress.CompleteTransfers()
	item.OutputPath = outPath
	item.Codec = "aac-lc"
	item.FileSize = outputFileSize(outPath)
	return nil
}
