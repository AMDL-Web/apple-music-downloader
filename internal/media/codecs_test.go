package media

import (
	"slices"
	"testing"

	"amdl/internal/config"
)

// The codecs a download tries, in order: the quality priority, then AAC-LC
// as the last fallback when codec_alternative is on; off, only the first.
func TestConfiguredCodecs(t *testing.T) {
	tests := []struct {
		name     string
		priority []string
		fallback bool
		want     []string
	}{
		{"fallback chain ends in AAC-LC", []string{"alac", "aac"}, true, []string{"alac", "aac", "aac-lc"}},
		{"no fallback keeps the first", []string{"alac", "aac-lc"}, false, []string{"alac"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := configuredCodecs(config.DownloadConfig{QualityPriority: tt.priority, CodecAlternative: tt.fallback})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("configuredCodecs() = %q, want %q", got, tt.want)
			}
		})
	}
}
