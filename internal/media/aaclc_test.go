package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"amdl/internal/config"
	"github.com/Eyevinn/mp4ff/mp4"
	widevine "github.com/iyear/gowidevine"
	wvpb "github.com/iyear/gowidevine/widevinepb"
)

func TestExtractAACLCMedia(t *testing.T) {
	kid := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES-CTR,URI=\"data:;base64," + kid + "\"\n#EXT-X-MAP:URI=\"audio.m4a\"\n"))
	}))
	defer server.Close()

	got, err := extractAACLCMedia(context.Background(), server.Client(), server.URL+"/track.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	if got.MediaURI != server.URL+"/audio.m4a" || got.KID != kid || got.KeyURI == "" {
		t.Fatalf("unexpected AAC-LC media: %+v", got)
	}
}

func TestMakeWidevinePSSH(t *testing.T) {
	kid := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	if _, err := makeWidevinePSSH(kid); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedWidevineDeviceLoads(t *testing.T) {
	kid := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	if _, _, err := newWidevineSession(kid); err != nil {
		t.Fatal(err)
	}
}

func TestWidevineDecryptStreamsRealCENCToFlatFile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	tmp := t.TempDir()
	plainPath := filepath.Join(tmp, "plain-fragmented.m4a")
	gen := exec.Command("ffmpeg", "-y", "-v", "error",
		"-f", "lavfi", "-i", "sine=frequency=523:duration=2",
		"-c:a", "aac", "-f", "mp4",
		"-movflags", "frag_keyframe+empty_moov+default_base_moof",
		"-frag_duration", "500000", plainPath)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate fragmented AAC: %v: %s", err, out)
	}
	plain, err := os.ReadFile(plainPath)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte("0123456789abcdef")
	iv := []byte("12345678")
	kid := mp4.UUID([]byte("fedcba9876543210"))
	fixture := buildEncryptedFixture(t, plain, key, iv, kid)
	keys := []*widevine.Key{{
		Type: wvpb.License_KeyContainer_CONTENT,
		ID:   kid,
		Key:  key,
	}}

	// The direct pipe helper must not consult Download.TempDir. Point it at a
	// regular file so the old flattenToFile path (MkdirTemp + fix-*/in.m4a)
	// would fail deterministically instead of creating and quickly removing an
	// intermediate that an after-the-fact directory scan could miss.
	blockedFixDir := filepath.Join(tmp, "must-not-create-fix-input")
	if err := os.WriteFile(blockedFixDir, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.Download.TempDir = blockedFixDir
	cfg.Tools.FFmpeg = "ffmpeg"
	p := newMP4Processor(cfg)
	flatPath := filepath.Join(tmp, "aaclc-flat.m4a")
	callbackCalled := false
	if err := p.decryptWidevineToFlatFile(context.Background(), bytes.NewReader(fixture.raw), keys, flatPath, func() {
		callbackCalled = true
	}); err != nil {
		t.Fatalf("stream Widevine decrypt to flat file: %v", err)
	}
	if !callbackCalled {
		t.Fatal("post-input remux callback was not called")
	}
	flat, err := os.ReadFile(flatPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(flat) < 12 || string(flat[8:12]) != "M4A " {
		t.Fatalf("flat output has invalid M4A brand: %q", flat[:min(len(flat), 12)])
	}
	if err := p.checkIntegrityFile(context.Background(), flatPath); err != nil {
		t.Fatalf("streamed AAC-LC output failed integrity check: %v", err)
	}

	// Decryption must be lossless: every AAC access unit and its duration
	// survives the decrypt+flatten unchanged. Compare the coded samples, not
	// decoded PCM: the flat file carries an edit list that newer ffmpeg (9.x)
	// honours by trimming the padding of the last frame, while the fragmented
	// source has none, so decoded PCM lengths differ by ffmpeg version even
	// though the audio is identical.
	got, want := readAudioSamples(t, flatPath), readAudioSamples(t, plainPath)
	if len(want) == 0 || len(got) != len(want) {
		t.Fatalf("streamed AAC-LC output has %d samples, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].dur != want[i].dur || !bytes.Equal(got[i].data, want[i].data) {
			t.Fatalf("sample %d differs: got %d bytes/dur %d, want %d bytes/dur %d",
				i, len(got[i].data), got[i].dur, len(want[i].data), want[i].dur)
		}
	}
	if fixDirs, err := filepath.Glob(filepath.Join(tmp, "fix-*")); err != nil {
		t.Fatal(err)
	} else if len(fixDirs) != 0 {
		t.Fatalf("AAC-LC streaming path created fix input intermediates: %v", fixDirs)
	}
}

func TestConfiguredCodecsUsesFallbackChain(t *testing.T) {
	cfg := config.DownloadConfig{
		QualityPriority: []string{"alac", "aac"}, CodecAlternative: true,
	}
	got, err := configuredCodecs(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alac", "aac", "aac-lc"}
	if len(got) != len(want) {
		t.Fatalf("configuredCodecs() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("configuredCodecs() = %#v, want %#v", got, want)
		}
	}
}

func TestConfiguredCodecsCanDisableFallback(t *testing.T) {
	got, err := configuredCodecs(config.DownloadConfig{
		QualityPriority: []string{"alac", "aac-lc"}, CodecAlternative: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "alac" {
		t.Fatalf("configuredCodecs() = %#v, want [alac]", got)
	}
}
