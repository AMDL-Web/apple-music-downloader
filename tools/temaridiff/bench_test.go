package temaridiff

import (
	"os"
	"testing"

	"amdl/internal/fairplay"
)

// BenchmarkDecryptSample measures single-goroutine throughput of both
// implementations on the golden sample, as a reference for performance work.
func BenchmarkDecryptSample(b *testing.B) {
	body, err := os.ReadFile(goldenTemplate)
	if err != nil {
		b.Fatal(err)
	}
	ct, err := os.ReadFile("../../internal/fairplay/testdata/track_1720704575_s1.ct")
	if err != nil {
		b.Fatal(err)
	}
	b.Run("fairplay", func(b *testing.B) {
		tmpl, err := fairplay.ParseTemplate(body)
		if err != nil {
			b.Fatal(err)
		}
		buf := make([]byte, len(ct))
		b.SetBytes(int64(len(ct)))
		for b.Loop() {
			copy(buf, ct)
			tmpl.DecryptSample(buf)
		}
	})
	b.Run("temari", func(b *testing.B) {
		up, err := loadUpstream(b).FromJSON(body)
		if err != nil {
			b.Fatal(err)
		}
		defer up.Close()
		b.SetBytes(int64(len(ct)))
		for b.Loop() {
			if _, err := up.Decrypt(ct); err != nil {
				b.Fatal(err)
			}
		}
	})
}
