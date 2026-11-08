package fairplay

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// t4 reads ctxTable.w, which holds a word at every 4-aligned ctx offset
// only, so every table base passed to t4 must be 4-aligned. An upstream
// release with an unaligned base fails here instead of reading wrong words.
func TestT4BasesAligned(t *testing.T) {
	call := regexp.MustCompile(`\bt4\(ctx, (0x[0-9a-fA-F]+|[0-9]+),`)
	any := regexp.MustCompile(`\bt4\(`)
	for _, name := range []string{"rounds_gen.go", "decrypt.go"} {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		calls := call.FindAllSubmatch(src, -1)
		if n := len(any.FindAll(src, -1)); n != len(calls) {
			t.Fatalf("%s: %d t4 calls, %d with a constant base", name, n, len(calls))
		}
		for _, m := range calls {
			base, err := strconv.ParseUint(string(m[1]), 0, 64)
			if err != nil {
				t.Fatal(err)
			}
			if base%4 != 0 || base >= ctxSize {
				t.Errorf("%s: t4 base %s is not a 4-aligned ctx offset", name, m[1])
			}
		}
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

// benchSizes are sample lengths around what a track carries: AAC samples are
// a few KiB, ALAC and Atmos samples tens of KiB, and 1 MiB shows the
// per-block cost without the per-sample one.
var benchSizes = []struct {
	name string
	n    int
}{{"4KiB", 4 << 10}, {"64KiB", 64 << 10}, {"1MiB", 1 << 20}}

// benchCiphertext tiles the golden ciphertext to n bytes. The rounds take the
// same path for any input, so the speed does not depend on the bytes.
func benchCiphertext(ct []byte, n int) []byte {
	b := make([]byte, n)
	for i := 0; i < n; i += len(ct) {
		copy(b[i:], ct)
	}
	return b
}

func BenchmarkDecryptSample(b *testing.B) {
	g := loadGolden(b)
	tmpl := mustParse(b, g.template)
	for _, size := range benchSizes {
		src := benchCiphertext(g.ct, size.n)
		b.Run(size.name, func(b *testing.B) {
			buf := make([]byte, len(src))
			b.SetBytes(int64(len(src)))
			for b.Loop() {
				copy(buf, src)
				tmpl.DecryptSample(buf)
			}
		})
	}
}

// BenchmarkDecryptSampleParallel runs one sample per goroutine on every
// GOMAXPROCS thread, as the wrapper's sample pool does.
func BenchmarkDecryptSampleParallel(b *testing.B) {
	g := loadGolden(b)
	tmpl := mustParse(b, g.template)
	for _, size := range benchSizes {
		src := benchCiphertext(g.ct, size.n)
		b.Run(size.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.RunParallel(func(pb *testing.PB) {
				buf := make([]byte, len(src))
				for pb.Next() {
					copy(buf, src)
					tmpl.DecryptSample(buf)
				}
			})
		})
	}
}
