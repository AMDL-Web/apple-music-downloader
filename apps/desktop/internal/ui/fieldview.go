package ui

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
)

// fieldEdit is what Settings remembers about the fields between frames.
type fieldEdit struct {
	// text is what a text field shows while it differs from the saved
	// value; focus names the fields that had the keyboard last frame.
	text  map[string]string
	focus map[string]bool
	// errs is why a value was not sent, by key.
	errs map[string]string
	// custom names the selects showing their Custom… box.
	custom map[string]bool
	// confirm is the change its dialog asks about, or nil.
	confirm *pendingChange
}

// pendingChange is a change that waits for its dialog's confirmation.
type pendingChange struct {
	title, body, verb string
	// detail is the new value, shown under the body.
	detail string
	danger bool
	apply  func()
	// open is the dialog's: Escape and a click beside it clear it too.
	open bool
}

// confirmField asks before f takes value: a downloads folder or file names
// that tracks saved under the old one are not found by.
func (a *App) confirmField(f *configField, value string) {
	e := &a.set.edit
	e.text[f.key] = value
	pc := &pendingChange{detail: value, open: true,
		title: "Change how files are named?", verb: "Change names",
		body: "Albums downloaded under the old names count as missing, so asking for them again downloads them again. Files already saved keep their names."}
	if f.kind == fieldPath {
		pc.title, pc.verb = "Change the downloads folder?", "Change folder"
		pc.body = "Tracks in the old folder are not found in the new one, so asking for them again downloads them again. Files already saved stay where they are."
	}
	pc.apply = func() {
		delete(e.text, f.key)
		a.Store.SetConfig(f.key, value)
	}
	e.confirm = pc
}

func (e *fieldEdit) init() {
	if e.text == nil {
		e.text, e.focus, e.errs, e.custom = map[string]string{}, map[string]bool{}, map[string]string{}, map[string]bool{}
	}
}

// valueText writes a configuration value as a field shows it.
func valueText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// strs is a list value as strings.
func strs(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		out = append(out, valueText(x))
	}
	return out
}

// fieldOff reports a field that applies only while another value allows
// it: shown dimmed, never hidden, so the section keeps its height.
func (a *App) fieldOff(f *configField) bool {
	cf := &a.Store.Config
	switch f.key {
	case "download.alac_max_sample_rate", "download.alac_max_bit_depth":
		return !slices.Contains(strs(cf.Value("download.quality_priority")), "alac")
	case "download.lyrics_timing", "download.lyrics_extras":
		on, _ := cf.Value("download.embed_lyrics").(bool)
		return !on && !slices.Contains(strs(cf.Value("download.lyrics_files")), "lrc")
	case "simulate.min_speed_kbps", "simulate.max_speed_kbps":
		on, _ := cf.Value("simulate.enabled").(bool)
		return !on
	}
	return false
}

// fieldRow is one field: its label and help on the left, the control on
// the right, or under them for the wide ones.
func (a *App) fieldRow(c *ui.Context, k *Tokens, f *configField) {
	cf := &a.Store.Config
	off := a.fieldOff(f)
	wide := f.kind == fieldPath || f.kind == fieldTemplate || f.kind == fieldToken || f.kind == fieldList
	row := ui.Column(c.Key(f.key)).Gap(10).PaddingY(12)
	if off {
		row.Opacity(0.45)
	}
	row.Children(func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, f.label).FontSize(13).FontWeight(600)
					if cf.Saving[f.key] {
						ui.Text(c, "Saving…").FontSize(11.5).TextColor(k.Faint)
					}
				})
				ui.Text(c, f.help).FontSize(11.5).TextColor(k.Faint).LineHeight(1.45)
			})
			if !wide {
				a.fieldControl(c, k, f, off)
			}
		})
		if wide {
			a.fieldControl(c, k, f, off)
		}
		msg := a.set.edit.errs[f.key]
		if msg == "" {
			msg = cf.Failed[f.key]
		}
		if msg != "" {
			ui.Text(c, msg).FontSize(12).TextColor(k.Bad).LineHeight(1.45)
		}
	})
}

func (a *App) fieldControl(c *ui.Context, k *Tokens, f *configField, off bool) {
	cf := &a.Store.Config
	v := cf.Value(f.key)
	switch f.kind {
	case fieldSwitch:
		on, _ := v.(bool)
		if toggle(c, k, f.key, &on, f.label).Disabled(off).Changed() {
			a.Store.SetConfig(f.key, on)
		}
	case fieldSelect:
		a.selectField(c, k, f, v, off)
	case fieldNumber:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			a.textField(c, k, f, valueText(v), 96, off, func(s string) {
				n, err := strconv.Atoi(s)
				switch {
				case err != nil || n < f.min || (f.max > 0 && n > f.max):
					a.set.edit.errs[f.key] = rangeText(f)
				default:
					a.Store.SetConfig(f.key, n)
				}
			})
			if f.unit != "" {
				ui.Text(c, f.unit).FontSize(12).TextColor(k.Muted).Width(34)
			}
		})
	case fieldPath, fieldTemplate:
		a.pathField(c, k, f, valueText(v))
	case fieldToken:
		a.tokenField(c, k, f, valueText(v))
	case fieldList:
		a.listField(c, k, f, strs(v))
	case fieldSet:
		a.setField(c, k, f, strs(v), off)
	}
}

func rangeText(f *configField) string {
	if f.max > 0 {
		return fmt.Sprintf("Write a whole number from %d to %d.", f.min, f.max)
	}
	return fmt.Sprintf("Write a whole number of at least %d.", f.min)
}

// textField is a one-line input that sends its text with commit on Return
// or when it loses the keyboard, unless it is the saved value. While it is
// not edited it shows saved.
func (a *App) textField(c *ui.Context, k *Tokens, f *configField, saved string, width float32, off bool, commit func(string)) ui.Element {
	e := &a.set.edit
	txt, editing := e.text[f.key]
	if !editing {
		txt = saved
	}
	box, in := fieldBox(c, k, f.key, &txt, "", true)
	in.Label(f.label)
	if width > 0 {
		box.Grow(0).Width(width)
	}
	in.Disabled(off)
	if in.Changed() {
		e.text[f.key] = txt
		delete(e.errs, f.key)
	}
	was := e.focus[f.key]
	e.focus[f.key] = in.Focused()
	if in.Submitted() || (was && !in.Focused()) {
		if _, edited := e.text[f.key]; edited {
			delete(e.text, f.key)
			if txt != saved {
				commit(txt)
			}
		}
	}
	return in
}

// selectField chooses among the options; a custom one also takes any other
// value matching its pattern through a box under it.
func (a *App) selectField(c *ui.Context, k *Tokens, f *configField, v any, off bool) {
	const customLabel = "Custom…"
	e := &a.set.edit
	cur := ""
	for _, o := range f.options {
		if valueText(o.value) == valueText(v) {
			cur = o.label
		}
	}
	showCustom := f.custom && (e.custom[f.key] || (cur == "" && v != nil))
	if showCustom {
		cur = customLabel
	}
	ui.Column(c).Gap(8).AlignItems(ui.End).Children(func() {
		sel := ui.SelectBase(c.Key(f.key), &cur)
		trigger := sel.Trigger.Width(220).Height(32).PaddingX(10).Gap(8).Radius(radiusControl).Background(k.Raise2).Border(1, k.Line2).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Label(f.label).Disabled(off)
		if sel.Open() || trigger.Focused() {
			trigger.Border(1, k.Accent)
		}
		trigger.Cursor(ui.CursorPointer).Children(func() {
			ui.Text(c, cur).FontSize(12.5).SingleLine()
			ui.Icon(c, icChevron).FontSize(12).TextColor(k.Muted)
		})
		sel.Popup(func(panel ui.Element) {
			panel.Width(220).Padding(padControl).Gap(1).Radius(radiusMenu).Background(k.Raise2).Border(1, k.Line2).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
			labels := make([]string, 0, len(f.options)+1)
			for _, o := range f.options {
				labels = append(labels, o.label)
			}
			if f.custom {
				labels = append(labels, customLabel)
			}
			for _, l := range labels {
				item := sel.Item(l).Height(28).PaddingX(10).Radius(radiusControl).AlignItems(ui.Center)
				col := k.Fg
				switch {
				case item.Highlighted():
					item.Background(k.Accent)
					col = k.AccentInk
				case l == cur:
					item.Background(k.Raise)
				}
				item.Cursor(ui.CursorPointer).Children(func() { ui.Text(c, l).FontSize(12.5).TextColor(col).SingleLine() })
			}
		})
		if trigger.Changed() {
			delete(e.custom, f.key)
			delete(e.errs, f.key)
			if cur == customLabel {
				e.custom[f.key] = true
			}
			for _, o := range f.options {
				if o.label == cur {
					a.Store.SetConfig(f.key, o.value)
				}
			}
		}
		if showCustom || cur == customLabel {
			saved := valueText(v)
			if cur == customLabel && e.custom[f.key] && slices.ContainsFunc(f.options, func(o option) bool { return valueText(o.value) == saved }) {
				saved = ""
			}
			in := a.textField(c, k, f, saved, 220, off, func(s string) {
				if f.pattern != nil && !f.pattern.MatchString(s) {
					e.errs[f.key] = f.hint + "."
					return
				}
				delete(e.custom, f.key)
				a.Store.SetConfig(f.key, s)
			})
			in.Placeholder(f.hint)
		}
	})
}

// pathField edits a folder or a file name template. A key that asks first
// shows Apply and Revert while it differs from the saved value; the others
// apply on Return or when the field loses the keyboard. In standalone mode
// the folders are this computer's, so a folder chooser helps.
func (a *App) pathField(c *ui.Context, k *Tokens, f *configField, saved string) {
	e := &a.set.edit
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		a.textField(c, k, f, saved, 0, false, func(s string) {
			if f.confirm {
				a.confirmField(f, s)
				return
			}
			a.Store.SetConfig(f.key, s)
		})
		draft, edited := e.text[f.key]
		if f.kind == fieldPath && a.Settings.Mode == mode.Standalone {
			if button(c, k, buttonPlain, "Choose…", nil).Height(32).Clicked() {
				start := saved
				if edited {
					start = draft
				}
				a.Act.ChooseFolder(start, func(dir string) {
					if dir == saved {
						delete(e.text, f.key)
						return
					}
					if f.confirm {
						a.confirmField(f, dir)
						return
					}
					a.Store.SetConfig(f.key, dir)
				})
			}
		}
		if f.confirm && edited && draft != saved {
			if button(c, k, buttonPrimary, "Apply…", nil).Height(32).Clicked() {
				a.confirmField(f, draft)
			}
			if button(c, k, buttonPlain, "Revert", nil).Height(32).Clicked() {
				delete(e.text, f.key)
			}
		}
	})
	if f.key == "download.downloads_dir" || f.key == "download.temp_dir" {
		a.folderState(c, k, f.key)
	}
}

// tokenField keeps a secret out of sight: it says whether one is set and
// takes a new one in a masked box.
func (a *App) tokenField(c *ui.Context, k *Tokens, f *configField, saved string) {
	e := &a.set.edit
	draft, replacing := e.text[f.key]
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if saved != "" && !replacing {
			pill(c, k, pillOK, icLock, "Set")
			ui.Spacer(c)
			if button(c, k, buttonPlain, "Replace", nil).Height(32).Clicked() {
				e.text[f.key] = ""
			}
			if button(c, k, buttonGhost, "Remove", nil).Height(32).Clicked() {
				a.Store.SetConfig(f.key, "")
			}
			return
		}
		box, in := fieldBox(c, k, f.key, &draft, "Paste the token", true)
		in.Password().Label(f.label)
		box.Grow(1)
		if in.Changed() {
			e.text[f.key] = draft
		}
		save := button(c, k, buttonPrimary, "Save", nil).Height(32).Disabled(draft == "")
		if (save.Clicked() || in.Submitted()) && draft != "" {
			delete(e.text, f.key)
			a.Store.SetConfig(f.key, draft)
		}
		if replacing && saved != "" && button(c, k, buttonPlain, "Cancel", nil).Height(32).Clicked() {
			delete(e.text, f.key)
		}
	})
}

// listField orders a list of options; each change applies at once.
func (a *App) listField(c *ui.Context, k *Tokens, f *configField, items []string) {
	labelOf := func(v string) string {
		for _, o := range f.options {
			if valueText(o.value) == v {
				return o.label
			}
		}
		return v
	}
	set := func(next []string) { a.Store.SetConfig(f.key, next) }
	ui.Column(c).Radius(radiusControl).Border(1, k.Line).Dividers(1, k.Line).Children(func() {
		for i, it := range items {
			ui.Row(c.Key(it)).Height(38).PaddingX(12).Gap(10).AlignItems(ui.Center).Children(func() {
				mono(c, strconv.Itoa(i+1), 11.5).TextColor(k.Faint).Width(14)
				ui.Text(c, labelOf(it)).FontSize(12.5).Grow(1).SingleLine()
				if len(items) < 2 {
					return
				}
				if iconButton(c, k, "up-"+it, icDown, "Move "+labelOf(it)+" up", i == 0).Rotate(180).Clicked() {
					next := slices.Clone(items)
					next[i-1], next[i] = next[i], next[i-1]
					set(next)
				}
				if iconButton(c, k, "down-"+it, icDown, "Move "+labelOf(it)+" down", i == len(items)-1).Clicked() {
					next := slices.Clone(items)
					next[i+1], next[i] = next[i], next[i+1]
					set(next)
				}
				if iconButton(c, k, "rm-"+it, icCross, "Remove "+labelOf(it), len(items) <= f.min).Clicked() {
					set(slices.Delete(slices.Clone(items), i, i+1))
				}
			})
		}
	})
	var rest []string
	for _, o := range f.options {
		if v := valueText(o.value); !slices.Contains(items, v) {
			rest = append(rest, o.label)
		}
	}
	if len(rest) == 0 {
		return
	}
	add := ""
	sel := ui.SelectBase(c.Key(f.key+"-add"), &add)
	sel.Trigger.AlignSelf(ui.Start).Height(30).PaddingX(10).Gap(6).Radius(radiusControl).Background(k.Raise).Border(1, k.Line2).AlignItems(ui.Center).
		Label("Add codec").Cursor(ui.CursorPointer).Children(func() {
		ui.Icon(c, icPlus).FontSize(13).TextColor(k.Muted)
		ui.Text(c, "Add codec").FontSize(12.5).SingleLine()
	})
	sel.Popup(func(panel ui.Element) {
		panel.MinWidth(200).Padding(padControl).Gap(1).Radius(radiusMenu).Background(k.Raise2).Border(1, k.Line2).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
		for _, l := range rest {
			item := sel.Item(l).Height(28).PaddingX(10).Radius(radiusControl).AlignItems(ui.Center)
			col := k.Fg
			if item.Highlighted() {
				item.Background(k.Accent)
				col = k.AccentInk
			}
			item.Cursor(ui.CursorPointer).Children(func() { ui.Text(c, l).FontSize(12.5).TextColor(col).SingleLine() })
		}
	})
	if sel.Trigger.Changed() {
		for _, o := range f.options {
			if o.label == add {
				set(append(slices.Clone(items), valueText(o.value)))
			}
		}
	}
}

// setField is a row of options to switch on and off; each change applies at
// once, in the options' order.
func (a *App) setField(c *ui.Context, k *Tokens, f *configField, items []string, off bool) {
	ui.Row(c).Gap(6).Children(func() {
		for _, o := range f.options {
			v := valueText(o.value)
			on := slices.Contains(items, v)
			b := ui.ButtonBase(c.Key(v)).Height(28).PaddingX(10).Gap(6).Radius(radiusControl).AlignItems(ui.Center).
				Label(o.label).Selected(on).Disabled(off)
			col := k.Muted
			if on {
				b.Background(k.AccentSoft).Border(1, k.Accent)
				col = k.Accent
			} else {
				b.Background(k.Raise2).Border(1, k.Line2)
			}
			if b.Clicked() {
				var next []string
				for _, p := range f.options {
					pv := valueText(p.value)
					if pv == v && !on || pv != v && slices.Contains(items, pv) {
						next = append(next, pv)
					}
				}
				if next == nil {
					next = []string{}
				}
				a.Store.SetConfig(f.key, next)
			}
			b.Cursor(ui.CursorPointer).Children(func() {
				if on {
					ui.Icon(c, icCheck).FontSize(12).TextColor(col)
				}
				ui.Text(c, o.label).FontSize(12.5).FontWeight(600).TextColor(col)
			})
		}
	})
}

// iconButton is a small square button with an icon and a name for
// assistive technology.
func iconButton(c *ui.Context, k *Tokens, key string, ic *ui.SVG, name string, disabled bool) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Size(26, 26).Radius(radiusInner).Center().Label(name).Tooltip(name).Disabled(disabled)
	col := k.Muted
	if disabled {
		col = k.Track
	} else if b.Hovered() {
		b.Background(k.Raise2)
		col = k.Fg
	}
	b.Cursor(ui.CursorPointer).Children(func() { ui.Icon(c, ic).FontSize(13).TextColor(col) })
	return b
}

// confirmDialog asks before a change that cannot be taken back, or that
// makes libraries download again, applies.
func (a *App) confirmDialog(c *ui.Context, k *Tokens) {
	e := &a.set.edit
	if e.confirm == nil {
		return
	}
	pc := e.confirm
	ui.DialogBase(c.Key("confirm"), &pc.open, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.45))
		panel.Width(460).Padding(padCard).Gap(12).Radius(radiusCard).Background(k.Raise).Border(1, k.Line2).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.4))
		ui.Text(c, pc.title).FontSize(15).FontWeight(650)
		ui.Text(c, pc.body).FontSize(12.5).TextColor(k.Muted).LineHeight(1.5)
		if pc.detail != "" {
			mono(c, pc.detail, 12).TextColor(k.Fg).MaxLines(3).Ellipsis("…")
		}
		ui.Row(c).Gap(8).Justify(ui.End).Padding(6, 0, 0, 0).Children(func() {
			if button(c, k, buttonPlain, "Cancel", nil).Clicked() {
				pc.open = false
			}
			ok := button(c, k, buttonPrimary, pc.verb, nil)
			if pc.danger {
				ok.Background(k.Bad)
			}
			if ok.Clicked() {
				pc.apply()
				pc.open = false
			}
		})
	})
	if !pc.open && e.confirm == pc {
		e.confirm = nil
	}
}
