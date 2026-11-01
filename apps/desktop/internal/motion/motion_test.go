package motion

import (
	"bytes"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"
)

// appleMaster is part of the master playlist of the test album's square
// motion cover, as mvod.itunes.apple.com serves it.
const appleMaster = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-I-FRAME-STREAM-INF:AVERAGE-BANDWIDTH=173111,BANDWIDTH=175343,VIDEO-RANGE=SDR,CODECS="avc1.64001f",RESOLUTION=486x486,URI="https://mvod.itunes.apple.com/x/P_Anull_trickPlay_gr210_sdr_486x486_iframes.m3u8"
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=2109203,BANDWIDTH=2327896,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.64001f",FRAME-RATE=24.000,RESOLUTION=768x768
https://mvod.itunes.apple.com/x/P_Anull_video_gr240_sdr_768x768.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=1549459,BANDWIDTH=1619026,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="hvc1.2.20000000.L123.B0",FRAME-RATE=24.000,RESOLUTION=768x768
https://mvod.itunes.apple.com/x/P_Anull_video_gr540_sdr_768x768.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=261853,BANDWIDTH=301384,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.64001f",FRAME-RATE=24.000,RESOLUTION=360x360
https://mvod.itunes.apple.com/x/P_Anull_video_gr203_sdr_360x360.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=362071,BANDWIDTH=403070,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.64001f",FRAME-RATE=24.000,RESOLUTION=408x408
https://mvod.itunes.apple.com/x/P_Anull_video_gr205_sdr_408x408.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=766367,BANDWIDTH=863074,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.64001f",FRAME-RATE=24.000,RESOLUTION=486x486
https://mvod.itunes.apple.com/x/P_Anull_video_gr210_sdr_486x486.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=1530132,BANDWIDTH=1727002,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.64001f",FRAME-RATE=24.000,RESOLUTION=486x486
https://mvod.itunes.apple.com/x/P_Anull_video_gr230_sdr_486x486.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=3827361,BANDWIDTH=4138938,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.640020",FRAME-RATE=24.000,RESOLUTION=1080x1080
https://mvod.itunes.apple.com/x/P_Anull_video_gr265_sdr_1080x1080.m3u8
#EXT-X-STREAM-INF:AVERAGE-BANDWIDTH=6542556,BANDWIDTH=7048968,VIDEO-RANGE=SDR,CLOSED-CAPTIONS=NONE,CODECS="avc1.640020",FRAME-RATE=24.000,RESOLUTION=1080x1080
https://mvod.itunes.apple.com/x/P_Anull_video_gr280_sdr_1080x1080.m3u8
`

// appleMedia is the media playlist of one of those renditions: one
// fragmented MP4 cut into byte ranges.
const appleMedia = `#EXTM3U
#EXT-X-TARGETDURATION:4
#EXT-X-VERSION:7
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-MAP:URI="P_Anull_video_gr220_sdr_486x486-.mp4",BYTERANGE="878@0"
#EXTINF:4.00000,
#EXT-X-BYTERANGE:538030@878
P_Anull_video_gr220_sdr_486x486-.mp4
#EXTINF:4.00000,
#EXT-X-BYTERANGE:524109@538908
P_Anull_video_gr220_sdr_486x486-.mp4
#EXTINF:4.00000,
#EXT-X-BYTERANGE:573376@1063017
P_Anull_video_gr220_sdr_486x486-.mp4
#EXTINF:4.00000,
#EXT-X-BYTERANGE:599447@1636393
P_Anull_video_gr220_sdr_486x486-.mp4
#EXT-X-ENDLIST
`

func TestPickFromApplesMaster(t *testing.T) {
	base, _ := url.Parse("https://mvod.itunes.apple.com/x/P_default.m3u8")
	vs := ParseMaster(appleMaster, base)
	if len(vs) != 8 {
		t.Fatalf("variants = %d, want the 8 video renditions without the trick-play one", len(vs))
	}
	for px, want := range map[int]string{
		512:  "P_Anull_video_gr240_sdr_768x768.m3u8",   // the only H.264 between 512 and 1080
		384:  "P_Anull_video_gr205_sdr_408x408.m3u8",   // smallest H.264 that is big enough
		486:  "P_Anull_video_gr230_sdr_486x486.m3u8",   // of two 486s, the higher bit rate
		2000: "P_Anull_video_gr280_sdr_1080x1080.m3u8", // none big enough: the best largest
	} {
		v, ok := Pick(vs, px)
		if !ok || !strings.HasSuffix(v.URL, want) {
			t.Errorf("Pick(%d) = %s, want …%s", px, v.URL, want)
		}
		if v.FrameRate != 24 {
			t.Errorf("Pick(%d) frame rate = %v, want 24", px, v.FrameRate)
		}
	}
}

func TestParseMediaJoinsApplesRanges(t *testing.T) {
	base, _ := url.Parse("https://mvod.itunes.apple.com/x/P_Anull_video_gr220_sdr_486x486.m3u8")
	got, err := parseMedia(appleMedia, base)
	if err != nil {
		t.Fatal(err)
	}
	// 878 bytes of init section, then four segments ending at
	// 1636393+599447: one range of the whole file.
	want := piece{url: "https://mvod.itunes.apple.com/x/P_Anull_video_gr220_sdr_486x486-.mp4", off: 0, n: 2235840}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("pieces = %+v, want %+v", got, want)
	}
}

func TestParseAttrsKeepsQuotedCommas(t *testing.T) {
	a := parseAttrs(`BANDWIDTH=1,CODECS="avc1.64001f,mp4a.40.2",RESOLUTION=2x4`)
	if a["CODECS"] != "avc1.64001f,mp4a.40.2" || a["RESOLUTION"] != "2x4" || a["BANDWIDTH"] != "1" {
		t.Fatalf("attrs = %v", a)
	}
}

func TestOutSizeKeepsTheShape(t *testing.T) {
	// The tall cover's 664x886 rendition at 384 wide: 886/664*384 = 512.4.
	if w, h := outSize(Variant{Width: 664, Height: 886}, 384); w != 384 || h != 512 {
		t.Fatalf("outSize = %dx%d, want 384x512", w, h)
	}
}

func TestFrameReaderReadsWholeFrames(t *testing.T) {
	// Two 2×1 RGBA frames, then half of a third.
	raw := bytes.Repeat([]byte{1, 2, 3, 4}, 2*2+1)
	f := newFrameReader(bytes.NewReader(raw), 2, 1, 40*time.Millisecond)
	for i := 0; i < 2; i++ {
		fr, err := f.Next()
		if err != nil || fr.Duration != 40*time.Millisecond || fr.Image.Bounds().Dx() != 2 {
			t.Fatalf("frame %d: %+v, %v", i, fr, err)
		}
	}
	if _, err := f.Next(); err != io.EOF {
		t.Fatalf("a cut frame: %v, want io.EOF", err)
	}
}
