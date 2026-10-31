package media

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
	"amdl/internal/wrapper"
)

// wordTimedLyrics has the shape wrapper-lite's /lyrics?syllable=1 returns:
// per-syllable spans, a background vocal (an untimed x-bg span around timed
// ones), a translation that carries the background vocal as an untimed span,
// a romanization split by syllable with Apple's double space between words,
// and a second line whose translation and romanization only repeat it.
const wordTimedLyrics = `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:itunes="http://music.apple.com/lyric-ttml-internal" xmlns:ttm="http://www.w3.org/ns/ttml#metadata" itunes:timing="Word" xml:lang="ja">` +
	`<head><metadata><iTunesMetadata xmlns="http://music.apple.com/lyric-ttml-internal">` +
	`<translations><translation type="subtitle" xml:lang="en-US">` +
	`<text for="L1">Hello there <span xmlns="http://www.w3.org/ns/ttml" ttm:role="x-bg">(hey)</span></text>` +
	`<text for="L2">same LINE</text>` +
	`</translation></translations>` +
	`<transliterations><transliteration xml:lang="ja-Latn">` +
	`<text for="L1"><span begin="1.000" end="1.400">ko</span><span begin="1.400" end="1.800">n</span>  <span begin="1.800" end="2.200">ni</span></text>` +
	`<text for="L2"><span begin="3.000" end="3.500">Same</span> <span begin="3.500" end="4.000">line</span></text>` +
	`</transliteration></transliterations>` +
	`</iTunesMetadata></metadata></head>` +
	`<body><div>` +
	`<p begin="1.000" end="2.600" itunes:key="L1" ttm:agent="v1"><span begin="1.000" end="1.400">こ</span><span begin="1.400" end="1.800">ん</span> <span begin="1.800" end="2.200">に</span> <span ttm:role="x-bg"><span begin="2.200" end="2.600">(ハイ)</span></span></p>` +
	`<p begin="3.000" end="4.000" itunes:key="L2" ttm:agent="v1"><span begin="3.000" end="3.500">Same</span> <span begin="3.500" end="4.000">line</span></p>` +
	`</div></body></tt>`

func TestLyricsLRCLineTimed(t *testing.T) {
	got, err := lyricsLRC(wordTimedLyrics, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "[00:01.00]こん に (ハイ)\n[00:03.00]Same line"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCLineTimedExtras(t *testing.T) {
	got, err := lyricsLRC(wordTimedLyrics, false, []string{"translation", "pronunciation"})
	if err != nil {
		t.Fatal(err)
	}
	// The background vocal stays in the translation; the syllables of a
	// romanized word stay together; L2's extras only repeat the line.
	want := "[00:01.00]こん に (ハイ)\n[00:01.00]Hello there (hey)\n[00:01.00]kon ni\n[00:03.00]Same line"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCWordTimedExtras(t *testing.T) {
	got, err := lyricsLRC(wordTimedLyrics, true, []string{"translation", "pronunciation"})
	if err != nil {
		t.Fatal(err)
	}
	want := "[00:01.00]<00:01.00>こ<00:01.40>ん <00:01.80>に <00:02.20>(ハイ)<00:02.60>\n" +
		"[00:01.00]Hello there (hey)\n" +
		"[00:01.00]<00:01.00>ko<00:01.40>n <00:01.80>ni<00:02.20>\n" +
		"[00:03.00]<00:03.00>Same <00:03.50>line<00:04.00>"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCWordTimingFallsBackToLinesWithoutSpans(t *testing.T) {
	raw := `<tt xmlns:itunes="http://music.apple.com/lyric-ttml-internal" itunes:timing="Line"><body><div>` +
		`<p begin="00:05.120" itunes:key="L1">only lines</p>` +
		`</div></body></tt>`
	got, err := lyricsLRC(raw, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[00:05.12]only lines"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCWordTimingWithoutParagraphBegin(t *testing.T) {
	raw := `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:itunes="http://music.apple.com/lyric-ttml-internal" itunes:timing="Word">` +
		`<body><div><p itunes:key="L1">` +
		`<span begin="20.626" end="20.942">初めて</span> ` +
		`<span begin="20.942" end="21.406">の</span>` +
		`</p></div></body></tt>`
	got, err := lyricsLRC(raw, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[00:20.62]<00:20.62>初めて <00:20.94>の<00:21.40>"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCTimeFormats(t *testing.T) {
	raw := `<tt><body><div>` +
		`<p begin="01:02:03.456">hour</p>` +
		`<p begin="02:03.400">minute</p>` +
		`<p begin="4.050">second</p>` +
		`</div></body></tt>`
	got, err := lyricsLRC(raw, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[62:03.45]hour\n[02:03.40]minute\n[00:04.05]second"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCTimingNoneOutputsPlainText(t *testing.T) {
	raw := `<tt itunes:timing="None"><body><div><p>first line</p><p>second line</p></div></body></tt>`
	got, err := lyricsLRC(raw, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "first line\nsecond line"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTTMLSidecarSkipsPlainTextLyrics(t *testing.T) {
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

func TestLyricsLRCPassesPlainTextThrough(t *testing.T) {
	got, err := lyricsLRC("first line\nsecond line\n", false, []string{"translation"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "first line\nsecond line"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCUsesTextAttributesNestedSpansAndEscapes(t *testing.T) {
	raw := `<tt><body><div><p begin="00:01.000" text="from attr"></p><p begin="00:02.000">hello <span>wide</span> &amp; clear</p></div></body></tt>`
	got, err := lyricsLRC(raw, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[00:01.00]from attr\n[00:02.00]hello wide & clear"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Older documents carry translations and transliterations as text attributes.
func TestLyricsLRCExtrasFromTextAttributes(t *testing.T) {
	raw := `<tt><head><metadata><iTunesMetadata>` +
		`<translations><translation><text for="L1" text="translated"></text></translation></translations>` +
		`<transliterations><transliteration><text for="L1" text="uta"></text></transliteration></transliterations>` +
		`</iTunesMetadata></metadata></head><body><div><p begin="00:01.000" itunes:key="L1">歌</p></div></body></tt>`
	got, err := lyricsLRC(raw, false, []string{"translation", "pronunciation"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "[00:01.00]歌\n[00:01.00]translated\n[00:01.00]uta"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLyricsLRCRejectsUntimedLines(t *testing.T) {
	_, err := lyricsLRC(`<tt><body><div><p>no time</p></div></body></tt>`, false, nil)
	if err == nil || !strings.Contains(err.Error(), "synchronised") {
		t.Fatalf("error = %v, want a missing-timing error", err)
	}
}

func TestLyricsLRCMalformedTTMLReturnsError(t *testing.T) {
	_, err := lyricsLRC(`<tt><body>`, false, nil)
	if err == nil || !strings.Contains(err.Error(), "lyrics") {
		t.Fatalf("error = %v, want lyrics context", err)
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
			if err := downloader.processTrack(context.Background(), domain.Job{ID: "job-1"}, item, applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter); !errors.Is(err, errStopAtWebPlayback) {
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
