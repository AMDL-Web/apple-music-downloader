package media

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"amdl/internal/limits"
)

const prefetchKey = "skd://itunes.apple.com/P000000000/s1/e1"

type m3u8Info = selectedMediaInfo

type variant struct {
	URI        string
	Audio      string
	Bandwidth  int
	BitDepth   int
	SampleRate int
}

type codecNotFoundError struct {
	Codec string
}

func (e codecNotFoundError) Error() string {
	return fmt.Sprintf("codec %s not found in manifest", e.Codec)
}

func (e codecNotFoundError) NonRetryable() bool {
	return true
}

var codecPatterns = map[string]*regexp.Regexp{
	"alac":         regexp.MustCompile(`audio-alac-stereo-\d{5,6}-\d{2}$`),
	"aac":          regexp.MustCompile(`audio-stereo-\d{3}$`),
	"aac-binaural": regexp.MustCompile(`audio-stereo-\d{3}-binaural$`),
	"aac-downmix":  regexp.MustCompile(`audio-stereo-\d{3}-downmix$`),
	"ec3":          regexp.MustCompile(`audio-(atmos|ec3)-\d{4}$`),
	"ac3":          regexp.MustCompile(`audio-ac3-\d{3}$`),
}

func extractMedia(ctx context.Context, httpClient *http.Client, masterURL, codec string, maxSampleRate, maxBitDepth int, gates ...*limits.RequestGate) (m3u8Info, error) {
	variants, err := fetchMasterVariants(ctx, httpClient, masterURL, gates...)
	if err != nil {
		return m3u8Info{}, err
	}
	selected, err := selectVariant(variants, codec, maxSampleRate, maxBitDepth)
	if err != nil {
		return m3u8Info{}, err
	}
	media, err := downloadText(ctx, httpClient, selected.URI, gates...)
	if err != nil {
		return m3u8Info{}, err
	}
	mediaURI, keys, err := parseMedia(media, selected.URI, codec)
	if err != nil {
		return m3u8Info{}, err
	}
	return m3u8Info{MediaURI: mediaURI, Keys: keys, CodecID: selected.Audio, BitDepth: selected.BitDepth, SampleRate: selected.SampleRate, Bandwidth: selected.Bandwidth}, nil
}

func fetchMasterVariants(ctx context.Context, httpClient *http.Client, masterURL string, gates ...*limits.RequestGate) ([]variant, error) {
	master, err := downloadText(ctx, httpClient, masterURL, gates...)
	if err != nil {
		return nil, err
	}
	return parseMaster(master, masterURL), nil
}

func selectVariant(variants []variant, codec string, maxSampleRate, maxBitDepth int) (variant, error) {
	var filtered []variant
	pat := codecPatterns[codec]
	for _, v := range variants {
		if pat != nil && !pat.MatchString(v.Audio) {
			continue
		}
		if codec == "alac" {
			if maxSampleRate > 0 && v.SampleRate > maxSampleRate {
				continue
			}
			if maxBitDepth > 0 && v.BitDepth > maxBitDepth {
				continue
			}
		}
		filtered = append(filtered, v)
	}
	if len(filtered) == 0 {
		return variant{}, codecNotFoundError{Codec: codec}
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Bandwidth > filtered[j].Bandwidth })
	return filtered[0], nil
}

func parseMaster(body, base string) []variant {
	sc := bufio.NewScanner(strings.NewReader(body))
	var out []variant
	var pending map[string]string
	mediaExtras := map[string]map[string]string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#EXT-X-MEDIA:") {
			attrs := parseAttrs(strings.TrimPrefix(line, "#EXT-X-MEDIA:"))
			if attrs["GROUP-ID"] != "" {
				mediaExtras[attrs["GROUP-ID"]] = attrs
			}
			continue
		}
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			pending = parseAttrs(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
			continue
		}
		if pending != nil && line != "" && !strings.HasPrefix(line, "#") {
			audio := pending["AUDIO"]
			extras := mediaExtras[audio]
			out = append(out, variant{
				URI: absURL(base, line), Audio: audio, Bandwidth: atoi(firstNonEmpty(pending["AVERAGE-BANDWIDTH"], pending["BANDWIDTH"])),
				BitDepth:   atoi(firstNonEmpty(extras["BIT-DEPTH"], extras["bit_depth"])),
				SampleRate: atoi(firstNonEmpty(extras["SAMPLE-RATE"], extras["sample_rate"])),
			})
			pending = nil
		}
	}
	return out
}

func parseMedia(body, base, codec string) (string, []string, error) {
	sc := bufio.NewScanner(strings.NewReader(body))
	keys := []string{prefetchKey}
	keySuffix := map[string]string{"alac": "c23", "aac": "c22", "aac-downmix": "c24", "aac-binaural": "c24", "ec3": "c24", "ac3": "c24"}[codec]
	if keySuffix == "" {
		keySuffix = "c6"
	}
	mediaURI := ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#EXT-X-KEY:") {
			attrs := parseAttrs(strings.TrimPrefix(line, "#EXT-X-KEY:"))
			key := attrs["URI"]
			if strings.HasPrefix(key, "skd://") && (strings.HasSuffix(key, keySuffix) || strings.HasSuffix(key, "c6")) {
				keys = append(keys, key)
			}
		}
		if strings.HasPrefix(line, "#EXT-X-MAP:") {
			attrs := parseAttrs(strings.TrimPrefix(line, "#EXT-X-MAP:"))
			if attrs["URI"] != "" {
				mediaURI = absURL(base, attrs["URI"])
			}
		}
	}
	if mediaURI == "" {
		return "", nil, fmt.Errorf("manifest has no EXT-X-MAP media URI")
	}
	return mediaURI, keys, sc.Err()
}

func parseAttrs(v string) map[string]string {
	out := map[string]string{}
	var key strings.Builder
	var val strings.Builder
	inKey := true
	inQuote := false
	flush := func() {
		k := strings.TrimSpace(key.String())
		value := strings.Trim(strings.TrimSpace(val.String()), `"`)
		if k != "" {
			out[k] = value
		}
		key.Reset()
		val.Reset()
		inKey = true
	}
	for _, r := range v {
		switch {
		case inKey && r == '=':
			inKey = false
		case !inKey && r == '"':
			inQuote = !inQuote
			val.WriteRune(r)
		case !inKey && r == ',' && !inQuote:
			flush()
		case inKey:
			key.WriteRune(r)
		default:
			val.WriteRune(r)
		}
	}
	flush()
	return out
}

func absURL(base, ref string) string {
	u, err := url.Parse(ref)
	if err == nil && u.IsAbs() {
		return ref
	}
	b, err := url.Parse(base)
	if err != nil {
		return ref
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return b.ResolveReference(r).String()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
