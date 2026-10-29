package fairplay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The golden vector is Temari's public test track 1720704575_s1 (template,
// ciphertext and plaintext), decoded from the embedded copies in
// bindings/go/temari_test.go at v0.5.1; MIT License, see doc.go.
type goldenVector struct {
	template, ct, pt []byte
}

func loadGolden(tb testing.TB) goldenVector {
	tb.Helper()
	read := func(ext string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "track_1720704575_s1."+ext))
		if err != nil {
			tb.Fatal(err)
		}
		return b
	}
	return goldenVector{template: read("json"), ct: read("ct"), pt: read("pt")}
}

func mustParse(tb testing.TB, body []byte) *Template {
	tb.Helper()
	tmpl, err := ParseTemplate(body)
	if err != nil {
		tb.Fatalf("ParseTemplate: %v", err)
	}
	return tmpl
}

func checkPlain(t *testing.T, got, want []byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("first mismatch at byte %d of %d", i, len(got))
		}
	}
}

func TestDecryptSampleGolden(t *testing.T) {
	g := loadGolden(t)
	tmpl := mustParse(t, g.template)
	buf := bytes.Clone(g.ct)
	tmpl.DecryptSample(buf)
	checkPlain(t, buf, g.pt)
}

// Each sample restarts the CBC chain from the template state, so any
// 16-byte-aligned prefix of the golden ciphertext decrypts to the same prefix
// of the plaintext, and whatever follows the aligned part is left alone.
func TestDecryptSamplePrefixes(t *testing.T) {
	g := loadGolden(t)
	tmpl := mustParse(t, g.template)
	for _, n := range []int{0, 1, 15, 16, 17, 31, 4096, 4101, 8192 + 9} {
		buf := bytes.Clone(g.ct[:n])
		tmpl.DecryptSample(buf)
		aligned := n / 16 * 16
		want := append(bytes.Clone(g.pt[:aligned]), g.ct[aligned:n]...)
		checkPlain(t, buf, want)
	}
}

func TestDecryptSampleConcurrent(t *testing.T) {
	g := loadGolden(t)
	tmpl := mustParse(t, g.template)
	bufs := make([][]byte, 8)
	var wg sync.WaitGroup
	for i := range bufs {
		bufs[i] = bytes.Clone(g.ct)
		wg.Add(1)
		go func(b []byte) {
			defer wg.Done()
			tmpl.DecryptSample(b)
		}(bufs[i])
	}
	wg.Wait()
	for _, b := range bufs {
		checkPlain(t, b, g.pt)
	}
}

func TestParseTemplate(t *testing.T) {
	g := loadGolden(t)
	var base map[string]any
	if err := json.Unmarshal(g.template, &base); err != nil {
		t.Fatal(err)
	}
	edit := func(f func(m map[string]any)) []byte {
		m := map[string]any{}
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
	tests := []struct {
		name    string
		body    []byte
		wantErr bool
	}{
		{"golden", g.template, false},
		{"not json", []byte("{ not json }"), true},
		{"no ctx", edit(func(m map[string]any) { delete(m, "ctx") }), true},
		{"no state", edit(func(m map[string]any) { delete(m, "state") }), true},
		{"short ctx", edit(func(m map[string]any) { m["ctx"] = "AAAA" }), true},
		{"short state", edit(func(m map[string]any) { m["state"] = "AAAA" }), true},
		{"ctx not base64", edit(func(m map[string]any) { m["ctx"] = "*" }), true},
		{"register not hex", edit(func(m map[string]any) { m["rcx"] = "0xzz" }), true},
		{"register empty", edit(func(m map[string]any) { m["rax"] = "" }), true},
		{"64-bit pointer register", edit(func(m map[string]any) { m["r9"] = "0x7f12345678ab" }), false},
		{"missing register reads as zero", edit(func(m map[string]any) { delete(m, "rbp") }), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseTemplate(tt.body)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func BenchmarkDecryptSample(b *testing.B) {
	g := loadGolden(b)
	tmpl := mustParse(b, g.template)
	buf := make([]byte, len(g.ct))
	b.SetBytes(int64(len(g.ct)))
	for b.Loop() {
		copy(buf, g.ct)
		tmpl.DecryptSample(buf)
	}
}
