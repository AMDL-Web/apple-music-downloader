package media

import (
	"fmt"
	"strings"

	"amdl/internal/config"
)

func configuredCodec(value string) (string, error) {
	codec := strings.ToLower(strings.TrimSpace(value))
	if codec == "" {
		return "", fmt.Errorf("codec in quality_priority cannot be empty")
	}
	switch codec {
	case "alac", "aac", "aac-binaural", "aac-downmix", "ec3", "ac3", "aac-lc":
		return codec, nil
	default:
		return "", fmt.Errorf("unsupported configured codec %q", value)
	}
}

func configuredCodecs(cfg config.DownloadConfig) ([]string, error) {
	values := append([]string(nil), cfg.QualityPriority...)
	if len(values) == 0 {
		return nil, fmt.Errorf("quality_priority must contain at least one codec")
	}
	if !cfg.CodecAlternative && len(values) > 1 {
		values = values[:1]
	}
	if cfg.CodecAlternative {
		values = append(values, "aac-lc")
	}
	codecs := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		codec, err := configuredCodec(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[codec]; exists {
			continue
		}
		seen[codec] = struct{}{}
		codecs = append(codecs, codec)
	}
	if len(codecs) == 0 {
		return nil, fmt.Errorf("quality_priority must contain at least one codec")
	}
	return codecs, nil
}

func fallbackCodec(codecs []string, selectedIndex int) string {
	if selectedIndex > 0 && len(codecs) > 0 {
		return codecs[0]
	}
	return ""
}

func codecFailureReason(err error) string {
	if err == nil {
		return "previous codec failed"
	}
	return err.Error()
}

func attemptsForCodec(configuredMaxAttempts, _ int) int {
	return configuredMaxAttempts
}
