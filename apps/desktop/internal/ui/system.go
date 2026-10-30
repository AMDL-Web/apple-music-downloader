package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/client"
	"amdl/apps/desktop/internal/format"
	"amdl/apps/desktop/internal/store"
)

// system is the backend's own state as Mole's cards: health, workers, what
// it downloaded, free space, the Apple Music account, library sync and
// hooks. The cards only read: the backend's settings, the account's
// sign-in and the library sync switch stay where they are configured.
func (a *App) system(c *ui.Context, k *Tokens, compact bool) {
	s := a.Store
	s.WatchServer()
	c.After(store.ServerEvery)
	sv := &s.Server
	if sv.Read.IsZero() && a.unreachable(c, k) {
		return
	}
	cols := 4
	if compact {
		cols = 2
	}
	ui.Scroll(c.Key("system")).Grow(1).Children(func() {
		ui.Grid(c).Columns(cols).Gap(14).Padding(6, 24, 20, 24).Children(func() {
			if sv.Read.IsZero() {
				// Grid cells size themselves: skeleton's full width would
				// overflow them.
				for i, span := range []int{2, 1, 1, 1, 1, 2} {
					el := ui.Box(c.Key(i)).Height(148).Radius(18).Background(k.Track).ColumnSpan(span)
					el.Opacity(0.55 + 0.45*el.Loop("sk", 1400*time.Millisecond, ui.Bounce(ui.EaseInOut)))
				}
				return
			}
			a.healthCard(c, k, sv)
			a.workersCard(c, k, sv)
			a.downloadedCard(c, k, sv)
			diskCard(c, k, "downloads", "Downloads folder", sv.DownloadsStorage())
			diskCard(c, k, "temp", "Temporary folder", sv.TempStorage())
			a.accountCard(c, k, sv)
			syncCard(c, k, sv)
			hooksCard(c, k, sv)
		})
	})
}

// moleCard is a card of the system screen: a colored icon square, the
// title, chips at the right, then body.
func moleCard(c *ui.Context, k *Tokens, key string, span int, tile ui.Color, ic *ui.SVG, title string, titleColor ui.Color, chips []string, body func()) {
	el := card(c.Key(key), k)
	if span > 1 {
		el.ColumnSpan(span)
	}
	if titleColor.A == 0 {
		titleColor = k.Muted
	}
	el.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(18, 18).Radius(5).Background(tile).Center().Children(func() {
				ui.Icon(c, ic).FontSize(11).TextColor(ui.RGB(255, 255, 255))
			})
			ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(titleColor).SingleLine()
			ui.Spacer(c)
			for _, ch := range chips {
				if ch != "" {
					chip(c, k, ch, ui.Color{})
				}
			}
		})
		body()
	})
}

// cardFoot is a card's last line of detail.
func cardFoot(c *ui.Context, k *Tokens, s string) {
	mono(c, s, 11.5).TextColor(k.Muted).Ellipsis("…")
}

func (a *App) healthCard(c *ui.Context, k *Tokens, sv *store.Server) {
	word, col, glow := "All good", k.OK, k.Accent
	var problems []string
	switch {
	case sv.Health == nil:
		word, col, glow = "Unreachable", k.Bad, k.Bad
		problems = append(problems, sv.HealthErr)
	default:
		if sv.Health.DatabaseError != "" {
			problems = append(problems, "database: "+sv.Health.DatabaseError)
		}
		if sv.Wrapper == nil || !sv.Wrapper.Ready {
			problems = append(problems, "the wrapper is not ready")
		}
		if sv.System != nil {
			for _, t := range sv.System.Tools {
				if !t.Available {
					problems = append(problems, t.Name+" is missing")
				}
			}
		}
		if sv.Health.Status != "ok" || len(problems) > 0 {
			word, col, glow = "Needs a look", k.Warn, k.Warn
		}
	}
	var chips []string
	foot := "Database, wrapper and ffmpeg all answer"
	if sv.System != nil {
		chips = []string{sv.System.Version, shortCommit(sv.System.Commit)}
		foot += " · up " + uptime(sv.System.UptimeSeconds)
	}
	if len(problems) > 0 {
		foot = strings.Join(problems, " · ")
	}
	moleCard(c, k, "health", 2, k.TileSystem, icCheck, "Health", col, chips, func() {
		ui.Row(c).Gap(18).AlignItems(ui.Center).Children(func() {
			display(c, word, 44).SingleLine()
			ui.Spacer(c)
			planet(c, k, glow)
		})
		cardFoot(c, k, foot)
	})
}

// planet is the health card's glowing sphere, lit from the top left in
// the state's color. It breathes slowly unless the system asks for less
// motion.
func planet(c *ui.Context, k *Tokens, glow ui.Color) {
	deep := ui.Hex("#05104b")
	p := ui.Box(c).Size(92, 92).Radius(999)
	breath := float32(0.5)
	if !c.Preferences().ReduceMotion {
		breath = p.Loop("breath", 5*time.Second, ui.Bounce(ui.EaseInOut))
	}
	p.Shadow(0, 0, 32+16*breath, 0, glow.Alpha(0.25+0.15*breath))
	p.LinearGradient(ui.LinearGradient{Angle: 135, Stops: []ui.GradientStop{
		{Color: ui.Hex("#f7ebf6"), At: 0},
		{Color: glow, At: 0.3},
		{Color: glow.Mix(deep, 0.7), At: 0.75},
		{Color: deep, At: 1},
	}})
	p.InsetShadow(0, 0, 0, 1, ui.RGBA(255, 255, 255, 0.08))
	if !k.Dark {
		p.Border(1, k.Line)
	}
}

func (a *App) workersCard(c *ui.Context, k *Tokens, sv *store.Server) {
	moleCard(c, k, "workers", 1, k.TileQueue, icQueue, "Workers", ui.Color{}, nil, func() {
		if sv.System == nil {
			bigValue(c, k, "—", "", 34)
			cardFoot(c, k, "The backend did not say")
			return
		}
		j := sv.System.Jobs
		bigValue(c, k, fmt.Sprint(j.Active), fmt.Sprintf("of %d busy", j.Workers), 34)
		vals := make([]float64, len(sv.Active))
		for i, v := range sv.Active {
			vals[i] = float64(v) / float64(max(j.Workers, 1))
		}
		bars(c, k, vals, 12, k.Accent)
		p := sv.System.Pools
		cardFoot(c, k, fmt.Sprintf("Queue %d/%d · pools %d download · %d decrypt", j.QueueDepth, j.QueueCapacity, p.Downloads, p.Decrypts))
	})
}

func (a *App) downloadedCard(c *ui.Context, k *Tokens, sv *store.Server) {
	moleCard(c, k, "downloaded", 1, k.TileLibrary, icNote, "Downloaded", ui.Color{}, []string{fmt.Sprintf("%d days", store.StatsDays)}, func() {
		if sv.Stats == nil {
			bigValue(c, k, "—", "", 34)
			cardFoot(c, k, "This backend keeps no statistics")
			return
		}
		tracks, failed, written := 0, 0, int64(0)
		top := 1
		for _, d := range sv.Stats.Daily {
			tracks += d.TracksCompleted
			failed += d.JobsFailed
			written += d.BytesWritten
			top = max(top, d.TracksCompleted)
		}
		bigValue(c, k, fmt.Sprint(tracks), "tracks", 34)
		vals := make([]float64, len(sv.Stats.Daily))
		for i, d := range sv.Stats.Daily {
			vals[i] = float64(d.TracksCompleted) / float64(top)
		}
		bars(c, k, vals, len(vals), k.TileLibrary)
		foot := format.Bytes(written) + " written"
		if failed > 0 {
			foot += " · " + plural(failed, "job") + " failed"
		}
		cardFoot(c, k, foot)
	})
}

// bars is a card's small column chart of vals (0..1), oldest first, in
// slots columns; slots without a value yet stay empty on the left.
func bars(c *ui.Context, k *Tokens, vals []float64, slots int, col ui.Color) {
	ui.Row(c).Height(26).Gap(3).AlignItems(ui.End).Children(func() {
		for i := range slots {
			v, empty := 0.0, true
			if j := i - (slots - len(vals)); j >= 0 {
				v, empty = vals[j], false
			}
			b := ui.Box(c.Key(i)).Grow(1).Radius(2).Height(max(2, float32(v)*26))
			if empty || v == 0 {
				b.Background(k.Track)
			} else {
				b.Background(col.Alpha(0.85))
			}
		}
	})
}

func diskCard(c *ui.Context, k *Tokens, key, title string, st *client.Storage) {
	var total string
	if st != nil && st.TotalBytes > 0 {
		total = format.Bytes(st.TotalBytes)
	}
	moleCard(c, k, key, 1, k.TileDisk, icDisk, title, ui.Color{}, []string{total}, func() {
		if st == nil || st.Error != "" || st.TotalBytes == 0 {
			bigValue(c, k, "—", "", 34)
			why := "The backend did not say"
			if st != nil && st.Error != "" {
				why = st.Error
			}
			cardFoot(c, k, why)
			return
		}
		v, unit := format.Scale(float64(st.FreeBytes))
		bigValue(c, k, format.Number(v, 2), unit+" free", 34)
		used := 1 - float64(st.FreeBytes)/float64(st.TotalBytes)
		meter(c, k, used, 8)
		cardFoot(c, k, fmt.Sprintf("%s · %d%% used", st.Path, int(used*100+0.5)))
	})
}

// meter is a bar of free space, filled to used (0..1) in the disk tiles'
// gold.
func meter(c *ui.Context, k *Tokens, used float64, h float32) {
	used = min(max(used, 0), 1)
	ui.Row(c).Height(h).Radius(h / 2).Background(k.Track).Clip().Children(func() {
		if used > 0 {
			ui.Box(c).Grow(float32(used)).Radius(h / 2).LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
				{Color: ui.Hex("#8a6a3e"), At: 0}, {Color: ui.Hex("#e2c08a"), At: 1},
			}})
		}
		if used < 1 {
			ui.Box(c).Grow(float32(1 - used))
		}
	})
}

func (a *App) accountCard(c *ui.Context, k *Tokens, sv *store.Server) {
	w := sv.Wrapper
	var chips []string
	if w != nil && len(w.Accounts) > 0 {
		chips = []string{plural(len(w.Accounts), "account")}
	}
	moleCard(c, k, "account", 2, k.TileSettings, icPerson, "Apple Music account", ui.Color{}, chips, func() {
		word, kind, state, ic := "Signed in", pillOK, "Ready", icCheck
		switch {
		case w == nil:
			word, kind, state, ic = "Unreachable", pillBad, "No answer", icCross
		case len(w.Accounts) == 0:
			word, kind, state, ic = "Signed out", pillWarn, "No account", icWarn
		case !w.Ready:
			kind, state, ic = pillWarn, "Not ready", icWarn
		}
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			display(c, word, 24).SingleLine()
			pill(c, k, kind, ic, state)
		})
		if w != nil && len(w.Accounts) > 0 {
			ui.Row(c).Gap(8).Wrap().Children(func() {
				for _, acc := range w.Accounts {
					ui.Row(c.Key(acc)).Height(28).PaddingX(10).Gap(6).Radius(8).Background(k.Raise2).Border(1, k.Line2).AlignItems(ui.Center).Children(func() {
						ui.Icon(c, icPerson).FontSize(12).TextColor(k.Faint)
						mono(c, acc, 12)
					})
				}
			})
		}
		switch {
		case w == nil:
			cardFoot(c, k, sv.WrapperErr)
		case len(w.Regions) > 0:
			cardFoot(c, k, "Storefronts "+strings.Join(w.Regions, ", ")+" · accounts are managed on the wrapper")
		default:
			cardFoot(c, k, "Accounts are managed on the wrapper")
		}
	})
}

func syncCard(c *ui.Context, k *Tokens, sv *store.Server) {
	l := sv.Sync
	var chips []string
	if l != nil && l.IntervalMinutes > 0 {
		chips = []string{fmt.Sprintf("%d min", l.IntervalMinutes)}
	}
	moleCard(c, k, "sync", 1, k.TileSystem, icSync, "Library sync", ui.Color{}, chips, func() {
		if l == nil {
			display(c, "—", 24)
			cardFoot(c, k, "This backend has no library sync")
			return
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if l.Enabled {
				display(c, "On", 24)
				ui.Spacer(c)
				pill(c, k, pillOK, icCheck, "Watching")
			} else {
				display(c, "Off", 24)
				ui.Spacer(c)
				pill(c, k, pillWait, nil, "Off in config")
			}
		})
		switch {
		case l.LastError != "":
			cardFoot(c, k, l.LastError)
		case l.LastPollAt == nil:
			cardFoot(c, k, "Not polled yet")
		case l.LastSubmitted == 0:
			cardFoot(c, k, "Last pass "+format.Clock(*l.LastPollAt)+" · nothing new")
		default:
			cardFoot(c, k, "Last pass "+format.Clock(*l.LastPollAt)+" · "+plural(l.LastSubmitted, "album")+" queued")
		}
	})
}

func hooksCard(c *ui.Context, k *Tokens, sv *store.Server) {
	h := sv.Hooks
	var chips []string
	if h != nil {
		chips = []string{fmt.Sprint(len(h.Entries))}
	}
	moleCard(c, k, "hooks", 1, k.TileQueue, icPlus, "Hooks", ui.Color{}, chips, func() {
		if h == nil || len(h.Entries) == 0 || !h.Enabled {
			display(c, "None", 24)
			foot := "No hooks in hooks.yaml"
			if h != nil && len(h.Entries) > 0 {
				foot = "Hooks are switched off in hooks.yaml"
			}
			cardFoot(c, k, foot)
			return
		}
		ran := 0
		var parts []string
		if sv.Stats != nil {
			for _, o := range sv.Stats.Hooks {
				ran += o.Succeeded + o.Failed
				if o.Failed > 0 {
					parts = append(parts, fmt.Sprintf("%s ✗ %d", o.Name, o.Failed))
				} else {
					parts = append(parts, o.Name+" ✓")
				}
			}
		}
		display(c, fmt.Sprintf("%d ran", ran), 24)
		if len(parts) == 0 {
			names := make([]string, 0, len(h.Entries))
			for _, e := range h.Entries {
				names = append(names, e.Name)
			}
			parts = []string{strings.Join(names, " · ") + " · none ran lately"}
		}
		cardFoot(c, k, strings.Join(parts, " · "))
	})
}

func shortCommit(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// uptime writes how long the backend has run: "19 min", "5 h", "3 days".
func uptime(secs int64) string {
	switch d := time.Duration(secs) * time.Second; {
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
