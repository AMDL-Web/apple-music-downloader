package temaridiff

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"amdl/internal/fairplay"

	temari "github.com/WorldObservationLog/Temari/bindings/go"
)

const (
	randomTemplates      = 48
	goldenStateTemplates = 24
	// goldenTemplate is Temari's public test template (MIT), shared with the
	// internal/fairplay golden test.
	goldenTemplate = "../../internal/fairplay/testdata/track_1720704575_s1.json"
)

func loadUpstream(tb testing.TB) *temari.Library {
	tb.Helper()
	lib, err := temari.LoadDefault()
	if err != nil {
		tb.Fatalf("load upstream Temari: %v", err)
	}
	return lib
}

func newRand(t *testing.T) (*rand.ChaCha8, int64) {
	t.Helper()
	seed := int64(1)
	if s, ok := os.LookupEnv("TEMARIDIFF_SEED"); ok {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("TEMARIDIFF_SEED=%q: %v", s, err)
		}
		seed = v
	}
	var key [32]byte
	binary.LittleEndian.PutUint64(key[:], uint64(seed))
	return rand.NewChaCha8(key), seed
}

func randomBytes(r *rand.ChaCha8, n int) []byte {
	b := make([]byte, n)
	_, _ = r.Read(b) // ChaCha8.Read never fails
	return b
}

// randomTemplate builds a /key data object with random tables and registers,
// and a random state unless state (base64) is given. Sizes vary around the
// real response's (32768-byte ctx, 8448-byte state) to cover the "at least"
// checks, and padding varies to cover both encodings.
func randomTemplate(r *rand.ChaCha8, state string) []byte {
	rr := rand.New(r)
	enc := base64.StdEncoding
	if rr.IntN(2) == 0 {
		enc = base64.RawStdEncoding
	}
	ctx := randomBytes(r, 0x8000+rr.IntN(4)*7)
	if state == "" {
		state = enc.EncodeToString(randomBytes(r, 0x2000+rr.IntN(0x200)))
	}
	body, err := json.Marshal(map[string]string{
		"ctx":   enc.EncodeToString(ctx),
		"state": state,
		"rcx":   fmt.Sprintf("0x%x", rr.Uint32()),
		"rax":   fmt.Sprintf("0x%x", rr.Uint32()),
		"rdx":   fmt.Sprintf("0x%x", rr.Uint32()),
		"r9":    fmt.Sprintf("0x%x", rr.Uint64()>>16),
		"rbp":   fmt.Sprintf("0x%x", rr.Uint64()>>16),
	})
	if err != nil {
		panic(err)
	}
	return body
}

// sampleSet covers empty and sub-block samples, block and 4 KiB page
// boundaries (the rounds address ciphertext by page), and random lengths.
func sampleSet(r *rand.ChaCha8) [][]byte {
	rr := rand.New(r)
	lengths := []int{0, 1, 15, 16, 17, 255, 4095, 4096, 4097, 8192 + 8, 65536 + 7}
	for range 8 {
		lengths = append(lengths, rr.IntN(20000))
	}
	var out [][]byte
	for _, n := range lengths {
		out = append(out, randomBytes(r, n))
	}
	return out
}

func decryptOurs(tmpl *fairplay.Template, sample []byte) (out []byte, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	out = bytes.Clone(sample)
	tmpl.DecryptSample(out)
	return out, nil
}

// compare decrypts every sample with both implementations and fails on the
// first difference. Errors and differences report positions only, never
// template or sample bytes, because real templates are key material.
func compare(t *testing.T, lib *temari.Library, label string, body []byte, samples [][]byte) {
	t.Helper()
	up, err := lib.FromJSON(body)
	if err != nil {
		t.Fatalf("%s: upstream rejected the template: %v", label, err)
	}
	defer up.Close()
	ours, err := fairplay.ParseTemplate(body)
	if err != nil {
		t.Fatalf("%s: ParseTemplate: %v", label, err)
	}
	for i, s := range samples {
		want, err := up.Decrypt(s)
		if err != nil {
			t.Fatalf("%s sample %d (%d bytes): upstream: %v", label, i, len(s), err)
		}
		got, err := decryptOurs(ours, s)
		if err != nil {
			t.Fatalf("%s sample %d (%d bytes): fairplay: %v", label, i, len(s), err)
		}
		if !bytes.Equal(got, want) {
			at := 0
			for at < len(got) && at < len(want) && got[at] == want[at] {
				at++
			}
			t.Fatalf("%s sample %d (%d bytes): differs from upstream at byte %d", label, i, len(s), at)
		}
	}
}

func TestRandomTemplatesMatchUpstream(t *testing.T) {
	lib := loadUpstream(t)
	r, seed := newRand(t)
	t.Logf("seed %d, %d templates", seed, randomTemplates)
	for i := range randomTemplates {
		compare(t, lib, fmt.Sprintf("template %d", i), randomTemplate(r, ""), sampleSet(r))
	}
}

// A random state puts most plaintext bytes at offsets outside their block, so
// the reconstruction drops them and a fully random template compares only a
// few bytes per block. Real states carry the block offsets (st[0x220] is 8 in
// the golden template), so these templates keep the golden state and
// randomize the tables and registers, which puts every byte on the line.
func TestGoldenStateTemplatesMatchUpstream(t *testing.T) {
	lib := loadUpstream(t)
	r, seed := newRand(t)
	golden, state := loadGolden(t)
	t.Logf("seed %d, golden template and %d variants", seed, goldenStateTemplates)
	compare(t, lib, "golden template", golden, sampleSet(r))
	for i := range goldenStateTemplates {
		compare(t, lib, fmt.Sprintf("golden-state template %d", i), randomTemplate(r, state), sampleSet(r))
	}
}

// loadGolden returns the golden template and its base64 state field.
func loadGolden(t *testing.T) ([]byte, string) {
	t.Helper()
	golden, err := os.ReadFile(goldenTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var fields struct{ State string }
	if err := json.Unmarshal(golden, &fields); err != nil {
		t.Fatal(err)
	}
	return golden, fields.State
}

// TestRealTemplatesMatchUpstream runs the same comparison on recorded /key
// responses, either the full {code, msg, data} envelope or the data object.
func TestRealTemplatesMatchUpstream(t *testing.T) {
	list := os.Getenv("TEMARIDIFF_TEMPLATE")
	if list == "" {
		t.Skip("TEMARIDIFF_TEMPLATE is not set")
	}
	lib := loadUpstream(t)
	r, _ := newRand(t)
	for _, path := range filepath.SplitList(list) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		body := raw
		if len(env.Data) > 0 {
			body = env.Data
		}
		compare(t, lib, filepath.Base(path), body, sampleSet(r))
	}
}

// TestParseMatchesUpstream checks that both sides accept and reject the same
// malformed templates, and agree on what an accepted one decrypts to.
func TestParseMatchesUpstream(t *testing.T) {
	lib := loadUpstream(t)
	r, _ := newRand(t)
	var base map[string]string
	_, state := loadGolden(t)
	if err := json.Unmarshal(randomTemplate(r, state), &base); err != nil {
		t.Fatal(err)
	}
	edit := func(f func(m map[string]string)) []byte {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		f(m)
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	shortCtx := base64.StdEncoding.EncodeToString(make([]byte, 0x8000-1))
	shortState := base64.StdEncoding.EncodeToString(make([]byte, 0x2000-1))
	cases := []struct {
		name string
		body []byte
	}{
		{"not json", []byte("{ not json }")},
		{"no ctx", edit(func(m map[string]string) { delete(m, "ctx") })},
		{"no state", edit(func(m map[string]string) { delete(m, "state") })},
		{"short ctx", edit(func(m map[string]string) { m["ctx"] = shortCtx })},
		{"short state", edit(func(m map[string]string) { m["state"] = shortState })},
		{"register not hex", edit(func(m map[string]string) { m["rdx"] = "0xzz" })},
		{"no rcx", edit(func(m map[string]string) { delete(m, "rcx") })},
		{"no r9 or rbp", edit(func(m map[string]string) { delete(m, "r9"); delete(m, "rbp") })},
		{"register without 0x", edit(func(m map[string]string) { m["rax"] = "1f" })},
	}
	samples := sampleSet(r)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			up, upErr := lib.FromJSON(c.body)
			if upErr == nil {
				up.Close()
			}
			_, ourErr := fairplay.ParseTemplate(c.body)
			if (upErr == nil) != (ourErr == nil) {
				t.Fatalf("upstream err = %v, fairplay err = %v", upErr, ourErr)
			}
			if upErr == nil {
				compare(t, lib, c.name, c.body, samples)
			}
		})
	}
}
