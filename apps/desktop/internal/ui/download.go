package ui

import (
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/format"
)

// downloadAsk is a download waiting for its sheet: which of two qualities
// to take, where the hit has both and the server's priority names both,
// and, for a hit amdl has downloaded, whether to download it again.
type downloadAsk struct {
	hit client.SearchHit
	// codecs are the choices in the server's priority order, the first
	// the default; nil asks nothing about quality.
	codecs   []string
	priority []string
	codec    int
	// again is how much of the hit is downloaded: MarkDone or MarkPartial,
	// MarkNone for nothing. A partial one chooses whether to fetch only
	// the missing tracks (all 0) or all of them again.
	again store.MarkState
	all   int
	open  bool
}

// download starts h, after a sheet when there is something to ask: the
// quality where the hit offers both lossless and Dolby and the server would
// take either, and whether to download again what is already downloaded.
// A paused hit resumes.
func (a *App) download(h client.SearchHit) {
	s := a.Store
	mark, _ := s.HitMark(h)
	if r := s.HitRecord(h); mark == store.MarkPaused && r != nil {
		s.Resume(domain.ResourceRef{Kind: r.Kind, ID: r.ID})
		return
	}
	priority := strs(s.Config.Value("download.quality_priority"))
	ask := downloadAsk{hit: h, priority: priority, codecs: format.QualityChoices(h.AudioTraits, priority)}
	if mark == store.MarkDone || mark == store.MarkPartial {
		ask.again = mark
	}
	if ask.codecs == nil && ask.again == store.MarkNone {
		s.SubmitHit(h, nil)
		return
	}
	ask.open = true
	a.asking = &ask
}

// openOrDownload opens the download in the queue a hit's mark names, or
// else downloads the hit.
func (a *App) openOrDownload(h client.SearchHit) {
	s := a.Store
	if mark, job := s.HitMark(h); mark == store.MarkActive && s.Job(job) != nil {
		a.OpenJob(job)
		return
	}
	if _, on := s.Search.Starting[store.HitKey(h)]; !on {
		a.download(h)
	}
}

// codecOrder is the chosen codec first, then the rest of the server's
// priority in its order; nil when the sheet asked nothing about quality.
func (d *downloadAsk) codecOrder() []string {
	if d.codecs == nil {
		return nil
	}
	first := d.codecs[d.codec]
	list := []string{first}
	for _, c := range d.priority {
		if c != first {
			list = append(list, c)
		}
	}
	return list
}

// overrides are a submit's: the codec order, nil without a choice.
func (d *downloadAsk) overrides() *config.DownloadOverrides {
	list := d.codecOrder()
	if list == nil {
		return nil
	}
	return &config.DownloadOverrides{QualityPriority: &list}
}

// replaces reports whether the answer downloads again what is downloaded,
// a reset of the hit's record, rather than adding what it lacks.
func (d *downloadAsk) replaces() bool {
	return d.again == store.MarkDone || (d.again == store.MarkPartial && d.all == 1)
}

// start does what the sheet's answer asks: a reset of the hit's record,
// a song's track alone, or a submit of the hit.
func (a *App) start(d *downloadAsk) {
	s := a.Store
	r := s.HitRecord(d.hit)
	if !d.replaces() || r == nil {
		s.SubmitHit(d.hit, d.overrides())
		return
	}
	var songs []string
	if d.hit.Type == "song" {
		songs = []string{d.hit.ID}
	}
	s.Reset(domain.ResourceRef{Kind: r.Kind, ID: r.ID}, songs, d.codecOrder(), hitName(d.hit))
}

// downloadSheet asks what download waits on: its quality, or whether to
// replace the files on disk, or both in one sheet.
func (a *App) downloadSheet(c *ui.Context, k *Tokens) {
	d := a.asking
	if d == nil {
		return
	}
	ui.DialogBase(c.Key("download"), &d.open, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(420).Padding(padCard).Gap(16).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		title := "Download “" + clip(hitName(d.hit), 44) + "”"
		if d.again == store.MarkDone {
			title += " again"
		}
		ui.Text(c, title).FontSize(15).FontWeight(650).MaxLines(2).Ellipsis("…")
		if d.codecs != nil {
			ui.Column(c).Gap(8).Children(func() {
				label(c, k, "Quality")
				names := make([]string, len(d.codecs))
				for i, cd := range d.codecs {
					names[i] = format.CodecName(cd)
				}
				// A row, so the track is as wide as its choices.
				ui.Row(c).Children(func() { segmented(c, k, "codec", &d.codec, names, nil) })
			})
		}
		switch d.again {
		case store.MarkPartial:
			ui.Column(c).Gap(8).Children(func() {
				label(c, k, "In your library")
				ui.Row(c).Children(func() {
					segmented(c, k, "all", &d.all, []string{"Missing tracks", "All tracks"}, nil)
				})
			})
		case store.MarkDone:
			ui.Text(c, "Replaces the downloaded files.").FontSize(13).TextColor(k.Muted)
		}
		ui.Row(c).Gap(8).Justify(ui.End).Padding(4, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				d.open = false
			}
			text := "Download"
			if d.replaces() {
				text = "Download again"
			}
			if button(c, k, buttonPrimary, text, icDown).Clicked() {
				a.start(d)
				d.open = false
			}
		})
	})
	if !d.open {
		a.asking = nil
	}
}
