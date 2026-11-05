package filmcheck

import (
	"bytes"
	"image"
	"strings"
	"testing"
)

func TestReportRendersAFakeFinding(t *testing.T) {
	before, bad := frame(), frame(image.Rect(100, 80, 140, 100))
	after := frame(image.Rect(100, 80, 140, 100))
	cases := []Case{
		{Num: "1", Title: "落地时突变", Where: "demo · light · 方向 open · 第 2 帧", Text: "pop at frame 2: <4 tiles> change",
			Frames: [3]*image.RGBA{before, bad, after}, Box: image.Rect(100, 80, 140, 100)},
		{Num: "2", Title: "整窗空白", Known: true, Why: "known", Text: "blank at frame 1", Frames: [3]*image.RGBA{nil, bad, nil}},
		{Num: "3", Title: "没有帧", Text: "stale"},
	}
	var buf bytes.Buffer
	if err := WriteReport(&buf, "看相发现", "lede", cases); err != nil {
		t.Fatal(err)
	}
	page := buf.String()
	for _, want := range []string{"pop at frame 2: &lt;4 tiles&gt; change", "data:image/png;base64,", "data:image/jpeg;base64,",
		"出问题那帧", "自动来回切", "放大 3 倍", "已放大 8 倍", "已知", "prefers-color-scheme:dark", "这一条没有留下帧图"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if strings.Contains(page, "ZgotmplZ") {
		t.Error("an image source was filtered out of the page")
	}
	// The 1x frames come before the zoomed ones.
	if strings.Index(page, `class="stage one"`) > strings.Index(page, `class="stage zoom"`) {
		t.Error("the zoomed frames come before the 1x ones")
	}
}
