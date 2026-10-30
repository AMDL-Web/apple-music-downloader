package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/widgets"
)

// qualityTabs are the new download screen's presets; the first keeps the
// backend's own download settings.
var qualityTabs = []store.Quality{store.QualityDefault, store.QualityHiRes, store.QualityLossless, store.QualityAtmos, store.QualityAAC}

// newDownload previews pasted links before they start: one row per link
// with its catalog metadata and whether it can start, the chosen one large
// on its album's color, and Mole's action bar with the one button.
func (a *App) newDownload(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	o := &s.Omni
	ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Padding(24, 28).Gap(14).Children(func() {
			label(c, k, fmt.Sprintf("Links · %d found", len(o.Links)))
			box := ui.Column(c).Height(92).Padding(10, 12).Radius(8).Background(k.Raise2).Border(1, k.Line2)
			box.Children(func() {
				a.linksTxt = o.Text
				in := ui.TextAreaBase(c.Key("links"), &a.linksTxt).Fill().Font(MonoFont).FontSize(12).LineHeight(1.7).
					Placeholder("Paste Apple Music links, one or many").Label("Links")
				if in.Changed() {
					s.SetOmniText(a.linksTxt)
				}
				if in.Focused() {
					box.Border(1, k.Accent).Shadow(0, 0, 0, 3, k.AccentSoft)
				}
			})
			if len(o.Links) == 0 {
				ui.Column(c).Grow(1).Center().Gap(6).Children(func() {
					ui.Text(c, "Album, playlist, song, artist and station links work.").TextColor(k.Muted)
					ui.Text(c, "Music videos and live radio are not downloaded.").FontSize(12).TextColor(k.Faint)
				})
				return
			}
			ui.Scroll(c.Key("link-rows")).Grow(1).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					for _, l := range o.Links {
						a.linkRow(c, k, l)
					}
				})
			})
			ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
				sel := 0
				for i, q := range qualityTabs {
					if q == o.Quality {
						sel = i
					}
				}
				labels := []string{"Server settings"}
				for _, q := range qualityTabs[1:] {
					labels = append(labels, store.QualityLabels[q])
				}
				segmented(c, k, "quality", &sel, labels, nil)
				o.Quality = qualityTabs[sel]
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					toggle(c, k, "overwrite", &o.Overwrite, "Overwrite existing files")
					ui.Text(c, "Overwrite").FontSize(12.5)
				})
			})
			a.actionBar(c, k)
		})
		if !compact {
			a.linkPreview(c, k)
		}
	})
}

// chosenLink is the link the preview shows: the one clicked, else the
// first that can start, else the first.
func (a *App) chosenLink() *store.Link {
	links := a.Store.Omni.Links
	for _, l := range links {
		if l.URL == a.newSel {
			return l
		}
	}
	for _, l := range links {
		if l.State == store.LinkReady && a.Store.Running(l) == nil {
			return l
		}
	}
	if len(links) > 0 {
		return links[0]
	}
	return nil
}

func (a *App) linkRow(c *ui.Context, k *Tokens, l *store.Link) {
	s := a.Store
	on := a.chosenLink() == l
	row := ui.ButtonBase(c.Key(l.URL)).Padding(10, 12).Gap(12).Radius(10).AlignItems(ui.Center).Justify(ui.Start).
		Background(k.Raise).Border(1, k.Line).Label(l.URL)
	if on {
		row.Border(1, k.Accent).Shadow(0, 0, 0, 3, k.AccentSoft)
	}
	if row.Clicked() {
		a.newSel = l.URL
	}
	lk := l.Lookup
	row.Cursor(ui.CursorPointer).Children(func() {
		pal := ParsePalette(lk.ArtworkBgColor, lk.ArtworkTextColor1, lk.ArtworkTextColor2, lk.ArtworkTextColor3, lk.ArtworkTextColor4)
		if l.State == store.LinkReady {
			widgets.Cover{Image: a.Art.Get(lk.ArtworkURL, 96), Placeholder: pal.Bg, Size: 48, Radius: 6, Color: 1}.Show(c)
		} else {
			ic := icLink
			if strings.Contains(l.URL, "/music-video/") {
				ic = icVideo
			}
			ui.Box(c).Size(48, 48).Radius(6).Background(k.Raise2).Center().Children(func() {
				ui.Icon(c, ic).FontSize(18).TextColor(k.Faint)
			})
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			title, meta := linkText(l)
			ui.Text(c, title).FontWeight(600).Ellipsis("…").SingleLine()
			mono(c, meta, 11).TextColor(k.Faint).Ellipsis("…")
		})
		switch {
		case l.State == store.LinkPending && s.Client() == nil:
			pill(c, k, pillWait, nil, "Not connected")
		case l.State == store.LinkPending:
			pill(c, k, pillWait, nil, "Looking up…")
		case l.State == store.LinkUnsupported:
			pill(c, k, pillBad, icCross, "Not supported")
		case l.State == store.LinkFailed:
			pill(c, k, pillBad, icCross, "Not found")
		case s.Running(l) != nil:
			pill(c, k, pillWarn, icWarn, "Already running")
		case refused(s, l) != "":
			pill(c, k, pillBad, icCross, "Refused")
		default:
			pill(c, k, pillOK, icCheck, "Ready")
		}
	})
}

// linkText is a link row's title and its line of facts.
func linkText(l *store.Link) (title, meta string) {
	lk := l.Lookup
	if l.State != store.LinkReady {
		u := l.URL
		if i := strings.Index(u, "music.apple.com/"); i >= 0 {
			u = u[i+len("music.apple.com/"):]
		}
		switch l.State {
		case store.LinkUnsupported:
			if strings.Contains(l.URL, "/music-video/") {
				return "Music video", u
			}
			return "Not a downloadable link", u
		case store.LinkFailed:
			return "Could not look it up", l.Err
		}
		return "Looking up…", u
	}
	parts := []string{}
	if by := firstNonEmptyStr(lk.ArtistName, lk.CuratorName); by != "" {
		parts = append(parts, by)
	}
	parts = append(parts, lk.Type)
	if lk.TrackCount > 0 {
		parts = append(parts, plural(lk.TrackCount, "track"))
	}
	parts = append(parts, lk.Storefront)
	return lk.Title, strings.Join(parts, " · ")
}

// refused is the backend's reason for not starting l in the last submit,
// or "".
func refused(s *store.Store, l *store.Link) string {
	r := s.Omni.Result
	if r == nil {
		return ""
	}
	for _, res := range r.Results {
		if res.URL == l.URL && res.Error != nil {
			return res.Error.Message
		}
	}
	return ""
}

// actionBar is Mole's bar: what will start on the left, one big button on
// the right.
func (a *App) actionBar(c *ui.Context, k *Tokens) {
	s := a.Store
	start := s.Startable()
	tracks, kinds := 0, map[string]int{}
	for _, l := range start {
		tracks += l.Lookup.TrackCount
		kinds[l.Lookup.Type]++
	}
	var running, unsupported int
	for _, l := range s.Omni.Links {
		switch {
		case l.State == store.LinkUnsupported || l.State == store.LinkFailed:
			unsupported++
		case s.Running(l) != nil:
			running++
		}
	}
	ui.Row(c).Padding(12, 14, 12, 16).Gap(14).Radius(16).Background(k.Raise2).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Children(func() {
			for i, l := range start {
				if i == 3 {
					break
				}
				pal := ParsePalette(l.Lookup.ArtworkBgColor, "", "", "", "")
				cv := ui.Box(c.Key(l.URL)).Radius(8).Border(2, k.Raise2)
				if i > 0 {
					cv.Margin(0, 0, 0, -10)
				}
				cv.Children(func() {
					widgets.Cover{Image: a.Art.Get(l.Lookup.ArtworkURL, 64), Placeholder: pal.Bg, Size: 30, Radius: 6, Color: 1}.Show(c)
				})
			}
		})
		ui.Column(c).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, summaryText(kinds, tracks)).FontSize(13).FontWeight(650).SingleLine()
			var notes []string
			if running > 0 {
				notes = append(notes, fmt.Sprintf("%d already running", running))
			}
			if unsupported > 0 {
				notes = append(notes, fmt.Sprintf("%d not supported", unsupported))
			}
			if pending := len(s.Omni.Links) - len(start) - running - unsupported; pending > 0 {
				notes = append(notes, fmt.Sprintf("%d not ready", pending))
			}
			if len(notes) > 0 {
				mono(c, strings.Join(notes, " · "), 11).TextColor(k.Muted)
			}
		})
		ui.Spacer(c)
		text := "Download"
		if n := len(start); n > 0 {
			text = "Download " + summaryNoun(kinds, n)
		}
		if s.Omni.Submitting {
			text = "Starting…"
		}
		b := button(c, k, buttonPill, text, nil).Disabled(len(start) == 0 || s.Omni.Submitting)
		if len(start) == 0 {
			b.Opacity(0.45)
		}
		if b.Clicked() {
			s.Submit()
		}
	})
}

// summaryText sums up what will start: "1 album · 12 tracks".
func summaryText(kinds map[string]int, tracks int) string {
	n := 0
	for _, v := range kinds {
		n += v
	}
	if n == 0 {
		return "Nothing ready to start"
	}
	s := summaryNoun(kinds, n)
	if tracks > 0 {
		s += " · " + plural(tracks, "track")
	}
	return s
}

// summaryNoun names n links by their type when they share one: "2 albums",
// else "3 links".
func summaryNoun(kinds map[string]int, n int) string {
	if len(kinds) == 1 {
		for t := range kinds {
			return plural(n, t)
		}
	}
	return plural(n, "link")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func firstNonEmptyStr(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// linkPreview shows the chosen link large, on its album's own color.
func (a *App) linkPreview(c *ui.Context, k *Tokens) {
	l := a.chosenLink()
	pane := ui.Column(c).Width(400).Padding(28).Gap(14).Clip()
	if l == nil || l.State != store.LinkReady {
		pane.Background(k.Raise).Center().Children(func() {
			ui.Icon(c, icLink).FontSize(28).TextColor(k.Faint)
			ui.Text(c, "The album you paste shows here.").TextColor(k.Muted)
		})
		return
	}
	lk := l.Lookup
	pal := ParsePalette(lk.ArtworkBgColor, lk.ArtworkTextColor1, lk.ArtworkTextColor2, lk.ArtworkTextColor3, lk.ArtworkTextColor4)
	bg := pane.AnimateColor("bg", pal.Bg, ui.Smooth)
	if !pal.OK {
		bg = k.Raise
	}
	t1, t2, t3 := Readable(pal.Text1, pal.Bg, k), Readable(pal.Text2, pal.Bg, k), Readable(pal.Text3, pal.Bg, k)
	pane.Background(bg).Children(func() {
		widgets.Cover{Image: a.Art.Get(lk.ArtworkURL, 700), Placeholder: pal.Bg.Mix(k.Ink, 0.2), Size: 344, Radius: 14, Color: 1, Shadow: true}.Show(c)
		var facts []string
		if lk.Type != "" {
			facts = append(facts, strings.ToUpper(lk.Type[:1])+lk.Type[1:])
		}
		if len(lk.ReleaseDate) >= 4 {
			facts = append(facts, lk.ReleaseDate[:4])
		}
		if lk.Genre != "" {
			facts = append(facts, lk.Genre)
		}
		label(c, k, strings.Join(facts, " · ")).TextColor(t3)
		display(c, lk.Title, 40).TextColor(t1).MaxLines(2).Ellipsis("…")
		if by := firstNonEmptyStr(lk.ArtistName, lk.CuratorName); by != "" {
			ui.Text(c, by).FontSize(15).TextColor(t2)
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if lk.TrackCount > 0 {
				tag(c, k, plural(lk.TrackCount, "track"), false, t2)
			}
			if lk.ContentRating == "explicit" {
				tag(c, k, "Explicit", false, t2)
			}
			if lk.AppleDigitalMaster {
				tag(c, k, "Apple Digital Master", false, t2)
			}
			if lk.NeedsMediaUserToken {
				tag(c, k, "Needs a media token", false, t2)
			}
		})
		if why := refused(a.Store, l); why != "" {
			ui.Text(c, why).FontSize(12.5).TextColor(t1).Padding(8, 10).Radius(8).Background(ui.RGBA(0, 0, 0, 0.18))
		}
	})
}
