package media

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/wrapper"
)

var updateLyrics = flag.Bool("update", false, "rewrite the lyrics golden files under testdata/lyrics")

// readLyricsInput is a lyrics document under testdata/lyrics, without the
// newline the file ends with. word_timed.ttml has the shape wrapper-lite's
// /lyrics?syllable=1 returns: per-syllable spans, a background vocal (an
// untimed x-bg span around timed ones), a translation that carries the
// background vocal as an untimed span, a romanization split by syllable with
// Apple's double space between words, and a second line whose translation
// and romanization only repeat it.
func readLyricsInput(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "lyrics", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(string(raw), "\n")
}

// TestLyricsLRC converts each document under testdata/lyrics to the .lrc
// named for its case; -update rewrites the .lrc files from the output.
func TestLyricsLRC(t *testing.T) {
	both := []string{"translation", "pronunciation"}
	tests := []struct {
		name   string
		input  string
		word   bool
		extras []string
	}{
		{"line_timed", "word_timed.ttml", false, nil},
		// The background vocal stays in the translation; the syllables of a
		// romanized word stay together; L2's extras only repeat the line.
		{"line_timed_extras", "word_timed.ttml", false, both},
		{"word_timed_extras", "word_timed.ttml", true, both},
		{"word_timing_without_spans", "lines_only.ttml", true, nil},
		{"word_no_paragraph_begin", "word_no_paragraph_begin.ttml", true, nil},
		{"time_formats", "time_formats.ttml", false, nil},
		{"timing_none", "timing_none.ttml", true, nil},
		{"plain_text_passthrough", "plain.txt", false, []string{"translation"}},
		{"text_attributes_and_escapes", "text_attributes.ttml", false, nil},
		// Older documents carry translations and transliterations as text
		// attributes.
		{"extras_from_text_attributes", "extras_text_attributes.ttml", false, both},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := lyricsLRC(readLyricsInput(t, tt.input), tt.word, tt.extras)
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "lyrics", tt.name+".lrc")
			if *updateLyrics {
				if err := os.WriteFile(golden, []byte(got+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if got != strings.TrimSuffix(string(want), "\n") {
				t.Fatalf("lyricsLRC(%s) =\n%s\nwant (%s):\n%s", tt.input, got, golden, want)
			}
		})
	}
}

func TestLyricsLRCErrors(t *testing.T) {
	tests := []struct{ name, doc, want string }{
		{"untimed lines", `<tt><body><div><p>no time</p></div></body></tt>`, "synchronised"},
		{"malformed TTML", `<tt><body>`, "lyrics"},
	}
	for _, tt := range tests {
		if _, err := lyricsLRC(tt.doc, false, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error = %v, want one mentioning %q", tt.name, err, tt.want)
		}
	}
}

func TestTTMLSidecarSkipsPlainTextLyrics(t *testing.T) {
	wordTimedLyrics := readLyricsInput(t, "word_timed.ttml")
	untimed := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:itunes="http://music.apple.com/lyric-ttml-internal" itunes:timing="None"><body><div><p>first line</p></div></body></tt>`
	cases := []struct {
		name string
		doc  string
		want string
	}{
		{name: "word timed", doc: wordTimedLyrics, want: wordTimedLyrics},
		{name: "untimed TTML", doc: untimed},
		{name: "bare plain text", doc: "first line\nsecond line"},
	}
	for _, tc := range cases {
		if got := ttmlSidecar(tc.doc); got != tc.want {
			t.Errorf("%s: sidecar = %q, want %q", tc.name, got, tc.want)
		}
	}
	lrc, err := lyricsLRC(untimed, false, nil)
	if err != nil || lrc != "first line" {
		t.Fatalf("untimed TTML keeps its .lrc and tag text: got %q, %v", lrc, err)
	}
}

// A song without synced lyrics has no word-timed document, and wrapper-lite
// falls back to plain text only for syllable=0. A miss on both is final, so
// it is not retried.
func TestFetchLyricsAsksForPlainTextOnceAfterAWordTimedMiss(t *testing.T) {
	notFound := &wrapper.ReplyError{Endpoint: "/lyrics", HTTPStatus: http.StatusOK, Code: http.StatusNotFound, Msg: "lyrics not found"}
	cases := []struct {
		name  string
		plain string
		want  domain.LyricsStatus
	}{
		{name: "plain text", plain: "first line\nsecond line", want: domain.LyricsFetched},
		{name: "no lyrics", want: domain.LyricsFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var syllables []bool
			cfg := aacLCRunConfig(t)
			cfg.Download.MaxAttempts = 3
			cfg.Download.EmbedCover = false
			downloader := newTestDownloader(&Downloader{
				cfg:     cfg,
				catalog: fakeDownloaderCatalog{song: applemusic.Song{ID: "song-1", Name: "One", HasLyrics: true}},
				wrapper: fakeDownloaderWrapper{webPlaybackErr: errStopAtWebPlayback, lyricsFor: func(opts wrapper.LyricsRequestOptions) (string, error) {
					syllables = append(syllables, opts.Syllable)
					if opts.Syllable || tc.plain == "" {
						return "", notFound
					}
					return tc.plain, nil
				}},
				tools: NewToolChecker(cfg.Tools),
			})
			reporter := &recordingReporter{}
			item := domain.JobItem{ID: "item-1", JobID: "job-1", AdamID: "song-1"}
			if err := downloader.processTrack(context.Background(), domain.Job{ID: "job-1"}, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, reporter); !errors.Is(err, errStopAtWebPlayback) {
				t.Fatalf("processTrack = %v, want the fake webplayback to stop it after the lyrics phase", err)
			}
			if want := []bool{true, false}; !slices.Equal(syllables, want) {
				t.Fatalf("requests by syllable = %v, want %v", syllables, want)
			}
			last := reporter.items[len(reporter.items)-1]
			if last.LyricsStatus != tc.want {
				t.Fatalf("lyrics_status = %q, want %q", last.LyricsStatus, tc.want)
			}
		})
	}
}
