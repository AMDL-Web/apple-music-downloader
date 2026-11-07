package ui

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

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

// strs is a list value as strings: as the server sends it, or as a change
// waiting for its answer holds it.
func strs(v any) []string {
	if l, ok := v.([]string); ok {
		return l
	}
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
	case "download.integrity_mode":
		on, _ := cf.Value("download.check_integrity").(bool)
		return !on
	}
	return false
}

// fieldHidden reports a field that has nothing to set now. The storefront
// applies only while no account is known, so it shows only once the
// accounts list is read and empty, together with the sign-in.
func (a *App) fieldHidden(f *configField) bool {
	if f.key == "catalog.default_storefront" {
		sv := &a.Store.Server
		return sv.Wrapper == nil || sv.Accounts == nil || len(sv.Accounts) > 0
	}
	return false
}

// fieldRow is one field: its label, and its help where it has one, on the
// left, the control on the right, or under them for the wide ones.
func (a *App) fieldRow(c *ui.Context, k *Tokens, f *configField) {
	cf := &a.Store.Config
	off := a.fieldOff(f)
	wide := f.kind == fieldPath || f.kind == fieldTemplate || f.kind == fieldText || f.kind == fieldToken || f.kind == fieldList
	row := ui.Column(c.Key(f.key)).Gap(10).PaddingY(12)
	if off {
		row.Opacity(0.45)
	}
	row.Children(func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Shrink(1).Gap(3).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, f.label).FontSize(13).FontWeight(600)
					// A save that lands quickly shows nothing: the word would
					// only flash.
					if since, ok := cf.SavingFor(f.key, c.Now()); ok {
						if since >= statusDelay {
							ui.Text(c, "Saving…").FontSize(11.5).TextColor(k.Faint)
						} else {
							c.After(statusDelay - since)
						}
					}
				})
				if f.help != "" {
					ui.Text(c, f.help).FontSize(11.5).TextColor(k.Faint).LineHeight(1.45)
				}
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
		a.textField(c, k, f, valueText(v), numberWidth(c, f), off, func(s string) {
			n, err := strconv.Atoi(s)
			switch {
			case err != nil || n < f.min || (f.max > 0 && n > f.max):
				a.set.edit.errs[f.key] = rangeText(f)
			default:
				a.Store.SetConfig(f.key, n)
			}
		})
	case fieldPath, fieldTemplate, fieldText:
		a.pathField(c, k, f, valueText(v), off)
	case fieldToken:
		a.tokenField(c, k, f, valueText(v))
	case fieldList:
		a.orderField(c, k, f, strs(v))
	case fieldSet:
		a.setField(c, k, f, strs(v), off)
	}
}

// numberWidth is a number field's width: room for the digits of its
// largest value (numberDigits without one) and its unit, which it holds at
// its right, so its edge lines up with the switches and menus above it.
func numberWidth(c *ui.Context, f *configField) float32 {
	digits := numberDigits
	if f.max > 0 {
		digits = len(strconv.Itoa(f.max))
	}
	w, _ := c.MeasureText(0, ui.Span{Text: strings.Repeat("0", digits), Font: MonoFont, Size: 12})
	w += 2*fieldPadX + 4
	if f.unit != "" {
		u, _ := c.MeasureText(0, ui.Span{Text: f.unit, Size: 12})
		w += fieldUnitGap + u
	}
	return float32(math.Ceil(float64(w)))
}

// numberDigits is the digits a number field without a largest value has
// room for.
const numberDigits = 6

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
	box, in := fieldBoxUnit(c, k, f.key, &txt, "", true, f.unit)
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
	// As wide as its longest option.
	width := float32(0)
	for _, o := range f.options {
		w, _ := c.MeasureText(0, ui.Span{Text: o.label, Size: 12.5})
		width = max(width, w)
	}
	if f.custom {
		w, _ := c.MeasureText(0, ui.Span{Text: customLabel, Size: 12.5})
		width = max(width, w)
	}
	width = float32(math.Ceil(float64(width + 2*fieldPadX + 8 + 12 + 2)))
	ui.Column(c).Gap(8).AlignItems(ui.End).Children(func() {
		sel := ui.SelectBase(c.Key(f.key), &cur)
		trigger := sel.Trigger.Width(width).Height(32).PaddingX(10).Gap(8).Radius(radiusControl).Background(k.Raise2).Border(1, k.Line2).
			AlignItems(ui.Center).Justify(ui.SpaceBetween).Label(f.label).Disabled(off)
		if sel.Open() {
			trigger.Border(1, k.Accent)
		}
		trigger.Cursor(ui.CursorPointer).Children(func() {
			ui.Text(c, cur).FontSize(12.5).SingleLine()
			ui.Icon(c, icChevron).FontSize(12).TextColor(k.Muted)
		})
		sel.Popup(func(panel ui.Element) {
			panel.MinWidth(width).Padding(padControl).Gap(1).Radius(radiusMenu).Background(k.Raise2).Border(1, k.Line2).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3))
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
func (a *App) pathField(c *ui.Context, k *Tokens, f *configField, saved string, off bool) {
	e := &a.set.edit
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		a.textField(c, k, f, saved, 0, off, func(s string) {
			if f.confirm {
				a.confirmField(f, s)
				return
			}
			a.Store.SetConfig(f.key, s)
		})
		draft, edited := e.text[f.key]
		if f.kind == fieldPath && a.Settings.Mode == mode.Standalone {
			if buttonOff(c, k, buttonPlain, "Choose…", nil, off).Height(32).Clicked() {
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
		box, in := fieldBox(c, k, f.key, &draft, "Paste the key", true)
		in.Password().Label(f.label)
		box.Grow(1)
		if in.Changed() {
			e.text[f.key] = draft
		}
		// The wrapper's key can be made here: it fills the field and goes
		// to the clipboard, for the gateway's side.
		if f.key == "wrapper.token" && button(c, k, buttonPlain, "Generate", nil).Height(32).Label("Generate a random key and copy it").Clicked() {
			if key := newWrapperKey(); key != "" {
				draft = key
				e.text[f.key] = key
				a.Act.CopyText(key)
			}
		}
		save := buttonOff(c, k, buttonPrimary, "Save key", nil, draft == "").Height(32)
		if (save.Clicked() || in.Submitted()) && draft != "" {
			delete(e.text, f.key)
			a.Store.SetConfig(f.key, draft)
		}
		if replacing && saved != "" && button(c, k, buttonPlain, "Cancel", nil).Height(32).Clicked() {
			delete(e.text, f.key)
		}
	})
}

// setField is a row of options to switch on and off; each change applies at
// once, in the options' order.
func (a *App) setField(c *ui.Context, k *Tokens, f *configField, items []string, off bool) {
	ui.Row(c).Gap(6).Children(func() {
		for _, o := range f.options {
			v := valueText(o.value)
			on := slices.Contains(items, v)
			b := ui.ButtonBase(c.Key(v)).Height(28).PaddingX(10).Radius(radiusControl).AlignItems(ui.Center).
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
			// The fill, border and ink say it is on; the label keeps one
			// weight, so choosing an option moves nothing.
			b.Cursor(ui.CursorPointer).Children(func() {
				ui.Text(c, o.label).FontSize(12.5).FontWeight(600).TextColor(col)
			})
		}
	})
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
