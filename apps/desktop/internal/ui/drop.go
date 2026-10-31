package ui

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

// linkDrop is what the window takes dropped from another app: links, as
// from a browser's address bar, and text, which may hold some.
var linkDrop = transfer.DropOptions{Formats: []transfer.Format{transfer.URIList, transfer.Text}}

// linkTarget makes root take links dropped from another app: linkOver says
// a drag is over it, and a drop goes to dropLinks. Files dragged from
// Finder come as a URI list too, and are no links.
func (a *App) linkTarget(c *ui.Context, root ui.Element) {
	offer, over := ui.DataDragOver(root, linkDrop)
	a.linkOver = over && !slices.Contains(offer.Formats, transfer.FileList)
	if d, ok := ui.DropData(root, linkDrop); ok {
		a.dropLinks(c.Now(), d.Data)
	}
}

// dropLinks takes what was dropped on the window. One Apple Music link
// downloads as a search hit does, after the same questions; several go to
// home's field, to start from its dock; anything else is ignored.
func (a *App) dropLinks(now time.Time, d transfer.Data) {
	links := store.FindLinks(droppedText(d))
	switch len(links) {
	case 0:
	case 1:
		url := links[0]
		a.dropping, a.dropSince = url, now
		a.Store.LookupOne(url, func(l client.Lookup) {
			if a.dropping != url {
				return // another drop came since
			}
			a.dropping = ""
			a.openOrDownload(lookupHit(url, l, a.Store.PriorJobFor(l)))
		})
	default:
		var urls []string
		for _, l := range a.Store.Omni.Links {
			urls = append(urls, l.URL)
		}
		a.Go(ScreenHome)
		a.Store.SetOmniText(strings.Join(append(urls, links...), "\n"))
	}
}

// droppedText is the text of a drop: its links and its plain text.
func droppedText(d transfer.Data) string {
	var parts []string
	if urls, err := d.URLs(); err == nil {
		parts = append(parts, urls...)
	}
	if b, err := d.Read(transfer.Text); err == nil {
		parts = append(parts, string(b))
	}
	return strings.Join(parts, "\n")
}

// lookupHit is a link's preview as the search hit it names, submitted by
// the link itself.
func lookupHit(url string, l client.Lookup, prior *client.PriorJob) client.SearchHit {
	id := l.ID
	if l.Type == "song" && l.TrackID != "" {
		id = l.TrackID
	}
	return client.SearchHit{
		ID: id, Type: l.Type, Name: l.Title, URL: url,
		ArtworkURL: l.ArtworkURL, ArtworkBgColor: l.ArtworkBgColor,
		ArtworkTextColor1: l.ArtworkTextColor1, ArtworkTextColor2: l.ArtworkTextColor2,
		ArtworkTextColor3: l.ArtworkTextColor3, ArtworkTextColor4: l.ArtworkTextColor4,
		ArtistName: l.ArtistName, AlbumName: l.AlbumName, ReleaseDate: l.ReleaseDate, Genre: l.Genre,
		TrackCount: l.TrackCount, DurationMS: l.DurationMS, ContentRating: l.ContentRating,
		AppleDigitalMaster: l.AppleDigitalMaster, AudioTraits: l.AudioTraits, PriorJob: prior,
	}
}

// dropZone is where links dragged over the window land, on a screen
// without home's field: a card lifted over the page.
func (a *App) dropZone(c *ui.Context, k *Tokens) {
	fade := &ui.Motion{Opacity: 0}
	veil := ui.Box(c.Key("drop")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Padding(24).Center().PassThrough().
		Background(k.Ground.Alpha(0.72)).Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Enter: fade, Exit: fade})
	veil.Children(func() {
		ui.Column(c.Key("drop-card")).Width(360).MaxWidthPercent(100).Height(176).Gap(12).Center().
			Radius(radiusCard).Background(k.Raise).Border(1.5, k.Accent).BorderStyle(ui.BorderDashed).
			Shadow(0, 24, 56, -12, ui.RGBA(0, 0, 0, 0.45)).
			Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Enter: &ui.Motion{Opacity: 0, Scale: 0.96, Y: 8}}).
			Children(func() {
				ui.Box(c).Size(44, 44).Radius(radiusFull).Background(k.AccentSoft).Center().Children(func() {
					ui.Icon(c, icLink).FontSize(22).TextColor(k.Accent)
				})
				ui.Text(c, "Drop to download").FontSize(15).FontWeight(650)
			})
	})
}
