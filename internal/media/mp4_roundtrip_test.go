package media

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"amdl/internal/config"
	"github.com/Eyevinn/mp4ff/mp4"
)

// These tests run the production container code (readInitSegment,
// prepareDecryptedInit, encodeDecryptedFragment, streamDecryptToWriter) on
// ffmpeg-made fragmented MP4s. The decrypt-and-decode differential test is
// mp4_codec_roundtrip_test.go.

// fragmentedAAC is a fragmented AAC file ffmpeg writes with extra, encrypted
// with CENC when extra asks for it.
func fragmentedAAC(t *testing.T, seconds string, extra ...string) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	src := filepath.Join(t.TempDir(), "frag.mp4")
	args := []string{"-y", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=" + seconds, "-c:a", "aac", "-b:a", "128k"}
	args = append(args, extra...)
	args = append(args, "-movflags", "frag_keyframe+empty_moov+default_base_moof", src)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate fragmented input: %v: %s", err, out)
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// identityDecrypt hands the samples back unchanged: the input is plaintext,
// or only the container is under test.
func identityDecrypt(_ string, samples [][]byte) ([][]byte, error) { return samples, nil }

// [regression] An init segment may carry optional boxes (here a free box)
// between ftyp and moov; the stream decrypt reads through them to the moov
// and on to every fragment.
func TestStreamDecryptAcceptsOptionalInitBoxesBeforeMoov(t *testing.T) {
	raw := fragmentedAAC(t, "1")
	ftypSize := int(binary.BigEndian.Uint32(raw[:4]))
	if ftypSize <= 0 || ftypSize > len(raw) || string(raw[4:8]) != "ftyp" {
		t.Fatalf("unexpected first box: size=%d type=%q", ftypSize, raw[4:8])
	}
	free := []byte{0, 0, 0, 8, 'f', 'r', 'e', 'e'}
	withFree := append(append(append([]byte{}, raw[:ftypSize]...), free...), raw[ftypSize:]...)

	init, _, err := readInitSegment(bytes.NewReader(withFree), 0)
	if err != nil || init.Moov == nil {
		t.Fatalf("readInitSegment with a free box before moov = %v (moov %v)", err, init != nil && init.Moov != nil)
	}
	p := newMP4Processor(config.Config{})
	var out bytes.Buffer
	if err := p.streamDecryptToWriter(context.Background(), bytes.NewReader(withFree), &out, []string{"key"}, identityDecrypt, nil); err != nil {
		t.Fatalf("streamDecryptToWriter: %v", err)
	}
	dir := t.TempDir()
	in, flat := filepath.Join(dir, "in.mp4"), filepath.Join(dir, "out.mp4")
	if err := os.WriteFile(in, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flat, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	want, got := readAudioSamples(t, in), readAudioSamples(t, flat)
	if len(got) == 0 || !slices.EqualFunc(got, want, func(a, b codedSample) bool { return a.dur == b.dur && bytes.Equal(a.data, b.data) }) {
		t.Fatalf("output carries %d samples, want the input's %d unchanged", len(got), len(want))
	}
}

// The decrypted container drops the encryption structure: on a real CENC
// fragmented MP4 with Apple's two-entry layout (two sample descriptions, the
// trex pointing at the second), prepareDecryptedInit turns enca back into
// mp4a, drops sinf and pssh and collapses the entries to one, and
// encodeDecryptedFragment removes each fragment's senc. The samples are fed
// back as-is (still ciphertext): only the container is under test.
func TestDecryptedContainerStripsEncryptionStructure(t *testing.T) {
	raw := fragmentedAAC(t, "2",
		"-encryption_scheme", "cenc-aes-ctr",
		"-encryption_key", "76a6c65c5ea762046bd749a2e632ccbb",
		"-encryption_kid", "a7e61c373e219033c21091fa607bf3b8")
	r := bufio.NewReader(bytes.NewReader(raw))
	init, offset, err := readInitSegment(r, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := audioSampleEntryType(t, init.Moov); got != "enca" {
		t.Fatalf("input sample entry = %q, want enca (test fixture not encrypted)", got)
	}
	stsdIn := init.Moov.Traks[0].Mdia.Minf.Stbl.Stsd
	stsdIn.Children = append(stsdIn.Children, stsdIn.Children[0])
	stsdIn.SampleCount = 2
	if init.Moov.Mvex == nil || len(init.Moov.Mvex.Trexs) == 0 {
		t.Fatal("input missing trex")
	}
	init.Moov.Mvex.Trexs[0].DefaultSampleDescriptionIndex = 2
	trex := init.Moov.Mvex.Trexs[0]

	if err := prepareDecryptedInit(init); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := init.Encode(&out); err != nil {
		t.Fatal(err)
	}
	for fi := 0; ; fi++ {
		var frag *mp4.Fragment
		frag, offset, err = readNextFragment(r, offset)
		if err != nil {
			t.Fatal(err)
		}
		if frag == nil {
			break
		}
		full, err := frag.GetFullSamples(trex)
		if err != nil {
			t.Fatal(err)
		}
		samples := make([][]byte, len(full))
		for i, s := range full {
			samples[i] = s.Data
		}
		if err := encodeDecryptedFragment(&out, fi, frag, samples); err != nil {
			t.Fatal(err)
		}
	}

	outFile, err := mp4.DecodeFile(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("decode decrypted container: %v", err)
	}
	stsd := outFile.Moov.Traks[0].Mdia.Minf.Stbl.Stsd
	if stsd.SampleCount != 1 || len(stsd.Children) != 1 {
		t.Fatalf("stsd not collapsed: SampleCount=%d children=%d", stsd.SampleCount, len(stsd.Children))
	}
	if idx := outFile.Moov.Mvex.Trexs[0].DefaultSampleDescriptionIndex; idx != 1 {
		t.Fatalf("trex default sample description index = %d, want 1 after stsd collapse", idx)
	}
	entry, ok := stsd.Children[0].(*mp4.AudioSampleEntryBox)
	if !ok {
		t.Fatalf("stsd child is %T, want *AudioSampleEntryBox", stsd.Children[0])
	}
	if entry.Type() != "mp4a" {
		t.Fatalf("sample entry type = %q, want mp4a (enca not stripped)", entry.Type())
	}
	if entry.Sinf != nil {
		t.Fatal("sinf box still present after decryption")
	}
	if outFile.Moov.Pssh != nil {
		t.Fatal("pssh box still present in moov")
	}
	if len(outFile.Segments) == 0 {
		t.Fatal("no fragments in the decrypted container")
	}
	for _, seg := range outFile.Segments {
		for _, frag := range seg.Fragments {
			for _, traf := range frag.Moof.Trafs {
				if traf.Senc != nil || traf.UUIDSenc != nil {
					t.Fatal("senc box still present in fragment")
				}
			}
		}
	}
}

func audioSampleEntryType(t *testing.T, moov *mp4.MoovBox) string {
	t.Helper()
	stsd := moov.Traks[0].Mdia.Minf.Stbl.Stsd
	if len(stsd.Children) == 0 {
		t.Fatal("empty stsd")
	}
	return stsd.Children[0].Type()
}
