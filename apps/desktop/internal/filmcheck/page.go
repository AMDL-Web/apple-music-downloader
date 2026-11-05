package filmcheck

import (
	"bytes"
	"encoding/base64"
	"html/template"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
)

// Case is one finding on the comparison page: the frame before it, the bad
// frame and the frame after, whole and at the window's own size.
type Case struct {
	// Num is the card's number, as "1" or "3b".
	Num string
	// Title is its headline; Where says the scene, theme, direction and
	// frame; Text is the finding as the tool writes it.
	Title, Where, Text string
	// Known marks a finding the maintainer has been told of; Why is the
	// reason the known file gives.
	Known bool
	Why   string
	// Frames are the frame before, the bad frame and the frame after; the
	// bad frame is required for the card to show frames, the others may be
	// nil at the edge of a film.
	Frames [3]*image.RGBA
	// Box is where the finding is, in the frame's pixels; empty for the
	// whole frame.
	Box image.Rectangle
}

const (
	pagePad       = 24   // pixels of ground kept around the box
	pageMaxWidth  = 1100 // widest a zoomed region is made, in pixels
	diffGain      = 8    // how much the difference image is amplified
	overviewWidth = 640  // the downscaled window is about this wide
)

type pageFrame struct {
	Label string
	One   template.URL // the region at its own size (1x)
	Zoom  template.URL // the region zoomed; empty when it is not
	On    bool
}

type pageCard struct {
	Num, Title, Where, Text, Why string
	Known                        bool
	Frames                       []pageFrame
	Diff, Overview               template.URL
	Zoom                         int
}

// WriteReport writes cases as one self-contained HTML page (every image
// inlined) on which each finding's frames can be flipped through: first
// the region at 1x, as it is seen, then zoomed, then the difference
// between the frame before and the bad one (amplified, and said to be) and
// the window with the region boxed. The page's own words are Chinese; a
// case's Text is not translated.
func WriteReport(w io.Writer, title, lede string, cases []Case) error {
	cards := make([]pageCard, 0, len(cases))
	for _, c := range cases {
		cards = append(cards, buildCard(c))
	}
	return pageTmpl.Execute(w, struct {
		Title, Lede string
		Cards       []pageCard
	}{title, lede, cards})
}

func buildCard(c Case) pageCard {
	card := pageCard{Num: c.Num, Title: c.Title, Where: c.Where, Text: c.Text, Known: c.Known, Why: c.Why}
	bad := c.Frames[1]
	if bad == nil {
		return card
	}
	b := bad.Bounds()
	box := c.Box.Intersect(b)
	whole := box.Empty() || box == b
	crop := b
	if !whole {
		crop = box.Inset(-pagePad).Intersect(b)
	}
	zoom := 1
	for _, z := range []int{3, 2} {
		if !whole && crop.Dx()*z <= pageMaxWidth {
			zoom = z
			break
		}
	}
	card.Zoom = zoom
	labels := [3]string{"前一帧", "出问题那帧", "后一帧"}
	var zoomed [3]*image.RGBA
	for i, f := range c.Frames {
		if f == nil || f.Bounds() != b {
			continue
		}
		pf := pageFrame{Label: labels[i], One: pngURI(Zoom(f, crop, 1)), On: i == 1}
		zoomed[i] = Zoom(f, crop, zoom)
		if zoom > 1 {
			pf.Zoom = pngURI(zoomed[i])
		}
		card.Frames = append(card.Frames, pf)
	}
	if zoomed[0] != nil {
		card.Diff = pngURI(Diff(zoomed[0], zoomed[1], diffGain))
	}
	if !whole {
		n := max(b.Dx()/overviewWidth, 1)
		small := Shrink(bad, n)
		r := image.Rect(box.Min.X/n, box.Min.Y/n, (box.Max.X+n-1)/n, (box.Max.Y+n-1)/n).Inset(-2)
		outline(small, r, color.RGBA{0xff, 0x3b, 0x30, 0xff})
		card.Overview = jpegURI(small)
	}
	return card
}

// Zoom is the part r of img, each pixel repeated z times each way.
func Zoom(img *image.RGBA, r image.Rectangle, z int) *image.RGBA {
	r = r.Intersect(img.Bounds())
	out := image.NewRGBA(image.Rect(0, 0, r.Dx()*z, r.Dy()*z))
	for y := 0; y < out.Rect.Dy(); y++ {
		src := img.PixOffset(r.Min.X, r.Min.Y+y/z)
		dst := out.PixOffset(0, y)
		for x := 0; x < r.Dx(); x++ {
			for k := 0; k < z; k++ {
				copy(out.Pix[dst:dst+4], img.Pix[src:src+4])
				dst += 4
			}
			src += 4
		}
	}
	return out
}

// Diff is the absolute difference of a and b, times gain, over black.
func Diff(a, b *image.RGBA, gain int) *image.RGBA {
	r := a.Bounds().Intersect(b.Bounds())
	out := image.NewRGBA(r)
	draw.Draw(out, r, image.NewUniform(color.Black), image.Point{}, draw.Src)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i, j, o := a.PixOffset(r.Min.X, y), b.PixOffset(r.Min.X, y), out.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			for k := 0; k < 3; k++ {
				d := int(a.Pix[i+k]) - int(b.Pix[j+k])
				if d < 0 {
					d = -d
				}
				out.Pix[o+k] = uint8(min(d*gain, 255))
			}
			i, j, o = i+4, j+4, o+4
		}
	}
	return out
}

// GrayRGBA is the w×h grey image pix as RGBA.
func GrayRGBA(pix []uint8, w, h int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		v := pix[i]
		out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = v, v, v, 255
	}
	return out
}

func pngURI(img image.Image) template.URL {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
}

func jpegURI(img image.Image) template.URL {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
		return ""
	}
	return template.URL("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()))
}

var pageTmpl = template.Must(template.New("page").Funcs(template.FuncMap{"idx": strconv.Itoa}).Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="claude:viewport-fit-sides" content="cover">
<title>{{.Title}}</title>
<style>
:root{--bg:#eef0f3;--card:#ffffff;--fg:#16181d;--muted:#5d6370;--line:#d9dde4;--accent:#b3261e;--btn:#f3f4f7;--btn-on:#16181d;--btn-on-fg:#ffffff;--stage:#e2e5ea;
--display:"Noto Sans SC",system-ui,-apple-system,sans-serif;--mono:"JetBrains Mono",ui-monospace,Menlo,monospace}
@media (prefers-color-scheme:dark){:root:not([data-theme="light"]){--bg:#111317;--card:#1a1d22;--fg:#eceef2;--muted:#9aa1ad;--line:#2b2f37;--accent:#ff6b5e;--btn:#23272e;--btn-on:#eceef2;--btn-on-fg:#111317;--stage:#0b0c0f;color-scheme:dark}}
:root[data-theme="dark"]{--bg:#111317;--card:#1a1d22;--fg:#eceef2;--muted:#9aa1ad;--line:#2b2f37;--accent:#ff6b5e;--btn:#23272e;--btn-on:#eceef2;--btn-on-fg:#111317;--stage:#0b0c0f;color-scheme:dark}
body{background:var(--bg);color:var(--fg);font-family:var(--display);line-height:1.55;margin:0}
main{max-width:980px;margin:0 auto;padding-block:28px 64px;padding-left:max(16px,env(safe-area-inset-left,0px));padding-right:max(16px,env(safe-area-inset-right,0px));display:flex;flex-direction:column;gap:20px}
h1{font-size:1.5rem;margin:0;text-wrap:balance}
.lede{color:var(--muted);margin:0;max-width:65ch}
.issue{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:18px;display:flex;flex-direction:column;gap:12px;min-width:0}
.issue header{display:flex;gap:12px;align-items:flex-start}
.num{font-family:var(--mono);font-weight:700;color:var(--accent);min-width:2ch}
.known .num{color:var(--muted)}
h2{font-size:1.1rem;margin:0}
.tag{font-size:.75rem;font-weight:400;color:var(--muted);border:1px solid var(--line);border-radius:4px;padding:1px 6px;margin-left:6px;vertical-align:middle}
.where{margin:2px 0 0;color:var(--muted);font-size:.85rem}
.what{margin:0;max-width:75ch;font-family:var(--mono);font-size:.85rem;overflow-wrap:anywhere}
.cap{margin:0;font-size:.8rem;color:var(--muted)}
.stage{background:var(--stage);border-radius:6px;overflow:auto;display:flex;justify-content:center;padding:8px}
.big{display:block;image-rendering:pixelated;max-width:100%;height:auto}
.big[hidden]{display:none}
.controls{display:flex;flex-wrap:wrap;gap:8px}
button{font:inherit;font-size:.9rem;padding:6px 12px;border-radius:6px;border:1px solid var(--line);background:var(--btn);color:var(--fg);cursor:pointer}
button[aria-pressed="true"]{background:var(--btn-on);color:var(--btn-on-fg);border-color:var(--btn-on)}
button:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.aux{display:grid;grid-template-columns:1fr 1fr;gap:12px}
@media (max-width:640px){.aux{grid-template-columns:1fr}}
figure{margin:0;min-width:0;display:flex;flex-direction:column;gap:6px}
figure img{border-radius:6px;border:1px solid var(--line);image-rendering:pixelated;background:#000;max-width:100%;height:auto}
figcaption{font-size:.8rem;color:var(--muted)}
</style>
</head>
<body>
<main>
  <h1>{{.Title}}</h1>
  {{with .Lede}}<p class="lede">{{.}}</p>{{end}}
{{range .Cards}}
<section class="issue{{if .Known}} known{{end}}">
  <header><span class="num">{{.Num}}</span><div><h2>{{.Title}}<span class="tag">{{if .Known}}已知{{else}}新发现{{end}}</span></h2><p class="where">{{.Where}}</p></div></header>
  <p class="what">{{.Text}}</p>
  {{with .Why}}<p class="where">已知原因：{{.}}</p>{{end}}
  {{if .Frames}}
  <div class="viewer">
    <p class="cap">原大（1x），就是你实际看到的样子</p>
    <div class="stage one">{{range $i, $f := .Frames}}<img class="big" data-i="{{idx $i}}" alt="{{$f.Label}}，原大" src="{{$f.One}}"{{if not $f.On}} hidden{{end}}>{{end}}</div>
    {{if gt .Zoom 1}}
    <p class="cap">放大 {{.Zoom}} 倍，只为看清位置，不代表肉眼看得到</p>
    <div class="stage zoom">{{range $i, $f := .Frames}}<img class="big" data-i="{{idx $i}}" alt="{{$f.Label}}，放大" src="{{$f.Zoom}}"{{if not $f.On}} hidden{{end}}>{{end}}</div>
    {{end}}
    <div class="controls" role="group" aria-label="切换帧">
      {{range $i, $f := .Frames}}<button type="button" data-i="{{idx $i}}"{{if $f.On}} aria-pressed="true"{{end}}>{{$f.Label}}</button>{{end}}
      <button type="button" class="play">自动来回切</button>
    </div>
  </div>
  <div class="aux">
    {{if .Diff}}<figure><img src="{{.Diff}}" alt="差异图（已放大 8 倍）"><figcaption>差异图，已放大 8 倍：前一帧和出问题那帧的差异，亮的地方就是变了的。放大后看着明显，不代表原大下看得出</figcaption></figure>{{end}}
    {{if .Overview}}<figure><img src="{{.Overview}}" alt="整窗位置"><figcaption>红框是上面放大的位置</figcaption></figure>{{end}}
  </div>
  {{else}}<p class="where">这一条没有留下帧图。</p>{{end}}
</section>
{{end}}
</main>
<script>
document.querySelectorAll('.viewer').forEach(function(v){
  var imgs=[].slice.call(v.querySelectorAll('.big')), btns=[].slice.call(v.querySelectorAll('button[data-i]')), play=v.querySelector('.play'), timer=null;
  var n=btns.length;
  var show=function(i){imgs.forEach(function(m){m.hidden=+m.dataset.i!==i});btns.forEach(function(b){b.setAttribute('aria-pressed',String(+b.dataset.i===i))})};
  var stop=function(){if(timer){clearInterval(timer);timer=null;play.setAttribute('aria-pressed','false');play.textContent='自动来回切'}};
  btns.forEach(function(b){b.addEventListener('click',function(){stop();show(+b.dataset.i)})});
  play.addEventListener('click',function(){
    if(timer){stop();return}
    var seq=n===3?[0,1,2,1]:[0,1],k=0;
    timer=setInterval(function(){show(seq[k++%seq.length])},450);
    play.setAttribute('aria-pressed','true');play.textContent='停';
  });
});
</script>
</body>
</html>
`))
