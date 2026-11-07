package media

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The HLS playlists Apple serves are parsed by hand (hls.go); these tables
// pin the parts of the format a download depends on.

func TestParseAttrs(t *testing.T) {
	tests := []struct {
		name, in string
		want     map[string]string
	}{
		{"plain and quoted", `BANDWIDTH=256000,AUDIO="audio-stereo-256"`, map[string]string{"BANDWIDTH": "256000", "AUDIO": "audio-stereo-256"}},
		{"comma inside quotes", `CODECS="mp4a.40.2,alac",AUDIO="a"`, map[string]string{"CODECS": "mp4a.40.2,alac", "AUDIO": "a"}},
		{"equals inside a value", `URI="https://cdn/k?x=1&y=2",METHOD=SAMPLE-AES`, map[string]string{"URI": "https://cdn/k?x=1&y=2", "METHOD": "SAMPLE-AES"}},
		{"spaces around keys and values", ` A = 1 , B="2"`, map[string]string{"A": "1", "B": "2"}},
		{"empty key dropped", `=x,A=1`, map[string]string{"A": "1"}},
		{"empty value kept", `A=,B=""`, map[string]string{"A": "", "B": ""}},
		{"unterminated quote runs to the end", `A="x,B=1`, map[string]string{"A": "x,B=1"}},
		{"empty", ``, map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAttrs(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseAttrs(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestAbsURL(t *testing.T) {
	const base = "https://aod.itunes.apple.com/itunes-assets/HLS/master.m3u8?a=1"
	tests := []struct{ name, base, ref, want string }{
		{"absolute ref kept", base, "https://other.example/m.m3u8", "https://other.example/m.m3u8"},
		{"sibling", base, "audio.m3u8", "https://aod.itunes.apple.com/itunes-assets/HLS/audio.m3u8"},
		{"sibling with its own query", base, "audio.m3u8?b=2", "https://aod.itunes.apple.com/itunes-assets/HLS/audio.m3u8?b=2"},
		{"parent dir", base, "../P1/a.mp4", "https://aod.itunes.apple.com/itunes-assets/P1/a.mp4"},
		{"host root", base, "/root.m3u8", "https://aod.itunes.apple.com/root.m3u8"},
		{"unparseable base keeps the ref", "://bad", "audio.m3u8", "audio.m3u8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := absURL(tt.base, tt.ref); got != tt.want {
				t.Fatalf("absURL(%q, %q) = %q, want %q", tt.base, tt.ref, got, tt.want)
			}
		})
	}
}

func TestParseMaster(t *testing.T) {
	const base = "https://cdn.example/hls/master.m3u8"
	tests := []struct {
		name string
		body string
		want []variant
	}{
		{
			name: "average bandwidth wins and the media group gives the quality",
			body: `#EXTM3U
#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio-alac-stereo-96000-24",NAME="ALAC",BIT-DEPTH=24,SAMPLE-RATE=96000
#EXT-X-STREAM-INF:BANDWIDTH=4000000,AVERAGE-BANDWIDTH=3600000,CODECS="alac",AUDIO="audio-alac-stereo-96000-24"
alac/96/media.m3u8
`,
			want: []variant{{URI: "https://cdn.example/hls/alac/96/media.m3u8", Audio: "audio-alac-stereo-96000-24", Bandwidth: 3600000, BitDepth: 24, SampleRate: 96000}},
		},
		{
			name: "bandwidth without an average, no media group, absolute URI",
			body: `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=256000,AUDIO="audio-stereo-256"
https://other.example/aac.m3u8
#EXT-X-STREAM-INF:AUDIO="audio-stereo-64"
aac64.m3u8
`,
			want: []variant{
				{URI: "https://other.example/aac.m3u8", Audio: "audio-stereo-256", Bandwidth: 256000},
				{URI: "https://cdn.example/hls/aac64.m3u8", Audio: "audio-stereo-64"},
			},
		},
		{
			name: "lower-case quality attributes",
			body: "#EXT-X-MEDIA:GROUP-ID=\"g\",bit_depth=16,sample_rate=44100\n#EXT-X-STREAM-INF:BANDWIDTH=1,AUDIO=\"g\"\ng.m3u8\n",
			want: []variant{{URI: "https://cdn.example/hls/g.m3u8", Audio: "g", Bandwidth: 1, BitDepth: 16, SampleRate: 44100}},
		},
		{
			name: "blank lines and tags between the stream and its URI, CRLF endings",
			body: "#EXTM3U\r\n#EXT-X-STREAM-INF:BANDWIDTH=2,AUDIO=\"a\"\r\n\r\n#EXT-X-SOMETHING\r\na.m3u8\r\n",
			want: []variant{{URI: "https://cdn.example/hls/a.m3u8", Audio: "a", Bandwidth: 2}},
		},
		{
			name: "a URI line without a stream is not a variant",
			body: "#EXTM3U\nstray.m3u8\n#EXT-X-STREAM-INF:BANDWIDTH=3,AUDIO=\"b\"\nb.m3u8\nafter.m3u8\n",
			want: []variant{{URI: "https://cdn.example/hls/b.m3u8", Audio: "b", Bandwidth: 3}},
		},
		{name: "no streams", body: "#EXTM3U\n#EXT-X-MEDIA:GROUP-ID=\"g\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseMaster(tt.body, base); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseMaster =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
}

func TestParseMedia(t *testing.T) {
	const base = "https://cdn.example/hls/alac/media.m3u8"
	const skd = "skd://itunes.apple.com/P000000000/s1/e1/"
	tests := []struct {
		name     string
		codec    string
		body     string
		wantURI  string
		wantKeys []string
		wantErr  string
	}{
		{
			name:  "the codec's keys and c6 follow the prefetch key",
			codec: "alac",
			body: `#EXTM3U
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="` + skd + `c23",KEYFORMAT="com.apple.streamingkeydelivery"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="` + skd + `c22"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="` + skd + `c6"
#EXT-X-KEY:METHOD=SAMPLE-AES,URI="data:text/plain;base64,AAAA",KEYFORMAT="urn:uuid:edef8ba9-79d6-4ace-a3c8-27dcd51d21ed"
#EXT-X-MAP:URI="P1/enc.mp4"
`,
			wantURI:  "https://cdn.example/hls/alac/P1/enc.mp4",
			wantKeys: []string{prefetchKey, skd + "c23", skd + "c6"},
		},
		{
			name:     "aac takes c22",
			codec:    "aac",
			body:     "#EXT-X-KEY:URI=\"" + skd + "c22\"\n#EXT-X-KEY:URI=\"" + skd + "c23\"\n#EXT-X-MAP:URI=\"a.mp4\"\n",
			wantURI:  "https://cdn.example/hls/alac/a.mp4",
			wantKeys: []string{prefetchKey, skd + "c22"},
		},
		{
			name:     "ec3 takes c24",
			codec:    "ec3",
			body:     "#EXT-X-KEY:URI=\"" + skd + "c24\"\n#EXT-X-MAP:URI=\"https://other.example/e.mp4\"\n",
			wantURI:  "https://other.example/e.mp4",
			wantKeys: []string{prefetchKey, skd + "c24"},
		},
		{
			name:     "a byte-range map keeps its URI",
			codec:    "alac",
			body:     "#EXT-X-MAP:URI=\"enc.mp4\",BYTERANGE=\"1024@0\"\n",
			wantURI:  "https://cdn.example/hls/alac/enc.mp4",
			wantKeys: []string{prefetchKey},
		},
		{name: "no map", codec: "alac", body: "#EXT-X-KEY:URI=\"" + skd + "c23\"\n", wantErr: "EXT-X-MAP"},
		{name: "map without a URI", codec: "alac", body: "#EXT-X-MAP:BYTERANGE=\"1@0\"\n", wantErr: "EXT-X-MAP"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, keys, err := parseMedia(tt.body, base, tt.codec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseMedia error = %v, want one mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if uri != tt.wantURI || !reflect.DeepEqual(keys, tt.wantKeys) {
				t.Fatalf("parseMedia = %q %q, want %q %q", uri, keys, tt.wantURI, tt.wantKeys)
			}
		})
	}
}

// Each codec picks only its own group IDs: the stereo AAC pattern must not
// take the binaural or downmix groups that share its prefix.
func TestSelectVariantMatchesTheCodecsGroups(t *testing.T) {
	variants := []variant{
		{Audio: "audio-alac-stereo-44100-16", Bandwidth: 1_000_000},
		{Audio: "audio-stereo-256", Bandwidth: 256_000},
		{Audio: "audio-stereo-64", Bandwidth: 64_000},
		{Audio: "audio-stereo-256-binaural", Bandwidth: 2_000_000},
		{Audio: "audio-stereo-256-downmix", Bandwidth: 3_000_000},
		{Audio: "audio-atmos-2768", Bandwidth: 2_768_000},
		{Audio: "audio-ac3-640", Bandwidth: 640_000},
	}
	for codec, want := range map[string]string{
		"alac":         "audio-alac-stereo-44100-16",
		"aac":          "audio-stereo-256",
		"aac-binaural": "audio-stereo-256-binaural",
		"aac-downmix":  "audio-stereo-256-downmix",
		"ec3":          "audio-atmos-2768",
		"ac3":          "audio-ac3-640",
	} {
		got, err := selectVariant(variants, codec, 0, 0)
		if err != nil || got.Audio != want {
			t.Errorf("selectVariant(%s) = %q, %v; want %q", codec, got.Audio, err, want)
		}
	}
	if _, err := selectVariant(variants[1:], "alac", 0, 0); !errors.As(err, new(codecNotFoundError)) {
		t.Errorf("alac without an ALAC group: error = %v, want codecNotFoundError", err)
	}
}

func TestSelectVariantAppliesDownloadConstraintsAndBandwidthOrder(t *testing.T) {
	variants := []variant{
		{Audio: "audio-alac-stereo-96000-16", Bandwidth: 4_000_000, SampleRate: 96_000, BitDepth: 16},
		{Audio: "audio-alac-stereo-48000-24", Bandwidth: 5_000_000, SampleRate: 48_000, BitDepth: 24},
		{Audio: "audio-alac-stereo-192000-24", Bandwidth: 9_000_000, SampleRate: 192_000, BitDepth: 24},
	}
	original := append([]variant(nil), variants...)

	tests := []struct {
		name          string
		maxSampleRate int
		maxBitDepth   int
		wantAudio     string
		wantNotFound  bool
	}{
		{name: "unlimited chooses highest bandwidth", wantAudio: "audio-alac-stereo-192000-24"},
		{name: "sample rate limit then bandwidth", maxSampleRate: 96_000, maxBitDepth: 24, wantAudio: "audio-alac-stereo-48000-24"},
		{name: "bit depth limit", maxBitDepth: 16, wantAudio: "audio-alac-stereo-96000-16"},
		{name: "all filtered", maxSampleRate: 44_100, maxBitDepth: 16, wantNotFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected, err := selectVariant(variants, "alac", tt.maxSampleRate, tt.maxBitDepth)
			var notFound codecNotFoundError
			if tt.wantNotFound {
				if !errors.As(err, &notFound) {
					t.Fatalf("error = %v, want codecNotFoundError", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if selected.Audio != tt.wantAudio {
				t.Fatalf("selected = %q, want %q", selected.Audio, tt.wantAudio)
			}
		})
	}
	if !reflect.DeepEqual(variants, original) {
		t.Fatalf("selectVariant mutated shared variants: got %#v want %#v", variants, original)
	}
}
