package tui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
	"amdl/internal/progress"
)

// Width breakpoints of the live region.
const (
	wideCols   = 100 // track rows carry a status column
	mediumCols = 72  // cards name the artist
	narrowCols = 48  // below this, cards drop their track rows
)

func (m *model) View() tea.View {
	w := max(m.width, 20)
	var blocks []string
	if m.height >= 12 {
		if c := m.cards(w); c != "" {
			blocks = append(blocks, c)
		}
	}
	if p := m.panelView(w); p != "" {
		blocks = append(blocks, p)
	}
	blocks = append(blocks, m.prompt(w), m.status(w))
	v := tea.NewView(strings.Join(blocks, "\n"))
	if pct, ok := m.overall(); ok {
		v.ProgressBar = tea.NewProgressBar(tea.ProgressBarDefault, pct)
	}
	return v
}

// overall is the mean progress of the running jobs, for the terminal's tab
// or taskbar progress.
func (m *model) overall() (int, bool) {
	n, sum := 0, 0
	for _, st := range m.jobs {
		if st.job.Status == domain.JobRunning {
			n++
			sum += m.album(st).Percent
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / n, true
}

func (m *model) album(st *jobState) progress.Album {
	if st.detail != nil {
		return progress.AlbumProgress(st.job.Status, st.detail.Items)
	}
	// A job not followed has only its counters: its finished tracks fill the
	// bar in outcome order.
	j := st.job
	var items []domain.JobItem
	add := func(n int, s domain.ItemStatus) {
		for i := 0; i < n; i++ {
			items = append(items, domain.JobItem{Index: len(items) + 1, Status: s})
		}
	}
	add(j.CompletedItems, domain.ItemCompleted)
	add(j.SkippedItems, domain.ItemSkipped)
	add(j.FailedItems, domain.ItemFailed)
	add(j.CancelledItems, domain.ItemCancelled)
	add(j.TotalItems-len(items), domain.ItemQueued)
	return progress.AlbumProgress(j.Status, items)
}

func (m *model) header() string {
	t := m.theme
	h := t.style(t.clay).Render("✻ ") + lipgloss.NewStyle().Foreground(t.fg).Bold(true).Render("amdl") +
		t.style(t.dim).Render(" "+m.opts.Version+" · "+m.opts.Target)
	if m.wrapper != nil {
		if what := wrapperProblem(*m.wrapper); what != "" {
			h += t.style(t.bad).Render(" · " + what)
		} else {
			h += t.style(t.dim).Render(" · wrapper ready (" + strings.Join(m.wrapper.Regions, ", ") + ")")
		}
	}
	tip := t.style(t.dim).Render("  paste a link to download, type to search, / for commands, ? for keys")
	return h + "\n" + tip
}

func (m *model) you(s string) string {
	return m.theme.style(m.theme.clay).Render("> ") + m.theme.style(m.theme.fg).Render(s)
}

func (m *model) reply(s string) string {
	return m.theme.style(m.theme.dim).Render("  ⎿ ") + s
}

func jobName(j domain.Job) string {
	if j.Title != "" {
		return j.Title
	}
	return j.Input
}

// finishedLine is what goes into scrollback when a job ends.
func (m *model) finishedLine(j domain.Job, d *client.DownloadDetail) string {
	t := m.theme
	mark, c := "✓ ", t.ok
	switch j.Status {
	case domain.JobFailed:
		mark, c = "✗ ", t.bad
	case domain.JobCancelled:
		mark, c = "⊘ ", t.dim
	}
	parts := []string{fmt.Sprintf("%d/%d saved", j.CompletedItems+j.SkippedItems, j.TotalItems)}
	if j.SkippedItems > 0 {
		parts = append(parts, fmt.Sprintf("%d already on disk", j.SkippedItems))
	}
	if j.FailedItems > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", j.FailedItems))
	}
	if d != nil {
		var size int64
		quality := ""
		for _, it := range d.Items {
			size += it.FileSize
			if quality == "" {
				quality = format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate)
			}
		}
		if quality != "" {
			parts = append(parts, quality)
		}
		if size > 0 {
			parts = append(parts, format.Bytes(size))
		}
	}
	if j.StartedAt != nil && j.FinishedAt != nil {
		parts = append(parts, format.Duration(j.FinishedAt.Sub(*j.StartedAt)))
	}
	if j.Status == domain.JobFailed && j.Error != "" {
		parts = append(parts, j.Error)
	}
	name := lipgloss.NewStyle().Foreground(t.fg).Bold(true).Render(jobName(j))
	if j.ArtistName != "" {
		name += t.style(t.dim).Render(" — " + j.ArtistName)
	}
	return t.style(c).Render(mark) + name + t.style(t.dim).Render(" · "+strings.Join(parts, " · "))
}

func (m *model) submitted(msg submitMsg) string {
	t := m.theme
	if msg.err != nil {
		return t.style(t.bad).Render("  ⎿ ") + errText(msg.err)
	}
	var lines []string
	for _, r := range msg.batch.Results {
		switch {
		case r.Job != nil:
			name := r.Job.Title
			if name == "" {
				name = m.titles[r.URL]
			}
			if name == "" {
				name = r.URL
			}
			lines = append(lines, "queued "+lipgloss.NewStyle().Foreground(t.fg).Bold(true).Render(name)+t.style(t.dim).Render(" · "+r.Job.ID))
		case r.ExistingJobID != "":
			lines = append(lines, t.style(t.dim).Render("already downloading · "+r.ExistingJobID))
		case r.Error != nil:
			lines = append(lines, t.style(t.bad).Render(r.Error.Message))
		}
	}
	return t.style(t.dim).Render("  ⎿ ") + strings.Join(lines, "\n    ")
}

func (m *model) jobTable(jobs []domain.Job) string {
	if len(jobs) == 0 {
		return "none"
	}
	t := m.theme
	var b strings.Builder
	for i, j := range jobs {
		if i > 0 {
			b.WriteString("\n    ")
		}
		c := t.dim
		switch j.Status {
		case domain.JobCompleted:
			c = t.ok
		case domain.JobFailed:
			c = t.bad
		case domain.JobRunning:
			c = t.info
		}
		b.WriteString(t.style(c).Render(fmt.Sprintf("%-9s", j.Status)) + " " +
			t.style(t.fg).Render(trunc(jobName(j), 40)) + t.style(t.dim).Render(fmt.Sprintf("  %d/%d  %s", j.CompletedItems+j.SkippedItems, j.TotalItems, j.ID)))
	}
	return b.String()
}

// cards draws the jobs in progress, the oldest running first.
func (m *model) cards(w int) string {
	ids := m.activeIDs()
	if len(ids) == 0 {
		return ""
	}
	maxCards, maxTracks := 3, 3
	if m.height < 20 {
		maxCards, maxTracks = 1, 2
	}
	if m.expanded {
		maxCards, maxTracks = 1, max(3, m.height-14)
	}
	if m.showAll {
		maxCards = len(ids)
		maxTracks = 0
	}
	var out []string
	for i, id := range ids {
		if i == maxCards {
			out = append(out, m.theme.style(m.theme.dim).Render(fmt.Sprintf("  +%d more · ctrl+t shows all", len(ids)-maxCards)))
			break
		}
		out = append(out, m.card(w, i+1, m.jobs[id], maxTracks))
	}
	return strings.Join(out, "\n")
}

func (m *model) card(w, n int, st *jobState, maxTracks int) string {
	t := m.theme
	j := st.job
	accent := t.accent(j.ArtworkBgColor)
	title := lipgloss.NewStyle().Foreground(t.fg).Bold(true).Render(trunc(jobName(j), max(10, w/2)))
	head := t.style(accent).Render("◆ ") + title
	if w >= mediumCols && j.ArtistName != "" {
		head += t.style(t.dim).Render(" · " + trunc(j.ArtistName, w/4))
	}
	right := ""
	if w >= mediumCols {
		right = string(j.Type)
		if q := m.quality(st); q != "" {
			right += " · " + q
		}
		right = t.style(t.dim).Render(fmt.Sprintf("%s  %d", right, n))
	}
	lines := []string{pad(head, w-lipgloss.Width(right)) + right}
	a := m.album(st)
	if j.Status == domain.JobQueued || a.Total == 0 {
		state := "queued"
		if j.Status == domain.JobRunning {
			state = "resolving tracks"
		}
		lines = append(lines, "  "+t.style(t.dim).Render(state))
		return strings.Join(lines, "\n")
	}
	eta := ""
	if w >= narrowCols {
		eta = format.Remaining(st.rate.Remaining())
	}
	lines = append(lines, "  "+albumLine(t, w-2, a, accent, eta))
	if w < narrowCols || st.detail == nil || maxTracks == 0 {
		return strings.Join(lines, "\n")
	}
	var rows []domain.JobItem
	for _, it := range st.detail.Items {
		switch it.Status {
		case domain.ItemQueued, domain.ItemCompleted, domain.ItemSkipped, domain.ItemCancelled:
			if !m.expanded {
				continue
			}
		}
		rows = append(rows, it)
	}
	sort.Slice(rows, func(i, k int) bool { return rows[i].Index < rows[k].Index })
	for i, it := range rows {
		if i == maxTracks {
			lines = append(lines, t.style(t.dim).Render(fmt.Sprintf("  … %d more · ctrl+o", len(rows)-maxTracks)))
			break
		}
		lines = append(lines, m.trackRow(w, it, accent))
	}
	return strings.Join(lines, "\n")
}

func (m *model) quality(st *jobState) string {
	if st.detail == nil {
		return ""
	}
	for _, it := range st.detail.Items {
		if q := format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate); q != "" {
			return q
		}
	}
	return ""
}

func (m *model) trackRow(w int, it domain.JobItem, accent color.Color) string {
	t := m.theme
	titleW := 24
	if w < mediumCols {
		titleW = 14
	}
	metaW := 0
	if w >= wideCols {
		metaW = 22
	} else if w >= mediumCols {
		metaW = 12
	}
	barW := w - 2 - 3 - titleW - 2
	if metaW > 0 {
		barW -= metaW + 1
	}
	num := t.style(t.dim).Render(fmt.Sprintf("%02d ", it.Index))
	title := t.style(t.fg).Render(pad(trunc(it.Title, titleW), titleW)) + "  "
	row := "  " + num + title + stageBar(t, barW, it, accent)
	if metaW > 0 {
		c := t.dim
		if it.Status == domain.ItemFailed {
			c = t.bad
		}
		row += " " + t.style(c).Render(trunc(itemState(it), metaW))
	}
	return row
}

func itemState(it domain.JobItem) string {
	p := it.Progress
	switch it.Status {
	case domain.ItemDownloading:
		if p.Download > 0 {
			return fmt.Sprintf("Download %d%%", int(p.Download*100))
		}
		return "Downloading"
	case domain.ItemDecrypting:
		if p.Decrypt > 0 {
			return fmt.Sprintf("Decrypt %d%%", int(p.Decrypt*100))
		}
		return "Decrypting"
	case domain.ItemWaitingDownload, domain.ItemWaitingDecrypt:
		return "Waiting for a slot"
	case domain.ItemResolving:
		return "Resolving"
	case domain.ItemRemuxing:
		return "Remuxing"
	case domain.ItemTagging:
		return "Tagging"
	case domain.ItemSaving:
		return "Saving"
	case domain.ItemCompleted:
		return "Saved"
	case domain.ItemSkipped:
		return "On disk"
	case domain.ItemFailed:
		if it.Error != "" {
			return "Failed: " + it.Error
		}
		return "Failed"
	case domain.ItemCancelled:
		return "Cancelled"
	}
	return "Queued"
}

func (m *model) panelView(w int) string {
	t := m.theme
	switch m.panel {
	case panelPreview:
		return m.previewView(w)
	case panelResults:
		return m.resultsView(w)
	case panelConfirm:
		return "  " + t.style(t.bad).Render(m.confirm.prompt) + t.style(t.dim).Render("  y / n")
	case panelLogin:
		hint := map[string]string{"username": "Apple ID", "password": "password (not shown)", "code": "two-step code from your device", "waiting": "…"}[m.loginSt.step]
		return "  " + t.style(t.dim).Render("sign in · "+hint+" · esc cancels")
	case panelHelp:
		return m.helpView(w)
	}
	if ms := m.menu(); len(ms) > 0 {
		var lines []string
		for i, s := range ms {
			name := t.style(t.fg)
			if i == 0 {
				name = lipgloss.NewStyle().Foreground(t.clay).Bold(true)
			}
			line := "  " + name.Render(pad(s.name, 10))
			if w >= narrowCols {
				line += t.style(t.dim).Render(trunc(strings.TrimSpace(s.args+"  "+s.help), w-14))
			}
			lines = append(lines, line)
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func (m *model) previewView(w int) string {
	t := m.theme
	var lines []string
	for _, p := range m.preview {
		if p.err != nil {
			lines = append(lines, "  "+t.style(t.bad).Render("✗ ")+t.style(t.fg).Render(trunc(p.link, w-6))+t.style(t.dim).Render(" · "+errText(p.err)))
			continue
		}
		l := p.l
		accent := t.accent(l.ArtworkBgColor)
		line := "  " + t.style(accent).Render("◆ ") + lipgloss.NewStyle().Foreground(t.fg).Bold(true).Render(trunc(l.Title, w/2))
		var facts []string
		if l.ArtistName != "" && w >= mediumCols {
			facts = append(facts, l.ArtistName)
		}
		facts = append(facts, l.Type)
		if len(l.ReleaseDate) >= 4 {
			facts = append(facts, l.ReleaseDate[:4])
		}
		if l.TrackCount > 0 {
			facts = append(facts, trackCount(l.TrackCount))
		}
		if l.AppleDigitalMaster && w >= wideCols {
			facts = append(facts, "Apple Digital Master")
		}
		lines = append(lines, line+t.style(t.dim).Render(" · "+strings.Join(facts, " · ")))
	}
	lines = append(lines, t.style(t.dim).Render("  enter downloads · esc dismisses"))
	return strings.Join(lines, "\n")
}

func (m *model) resultsView(w int) string {
	t := m.theme
	r := m.res
	head := "search · " + r.term
	if r.artist != nil {
		head = r.artist.Name + " · albums"
	}
	vis := r.visible()
	var tabs []string
	if r.artist == nil && w >= narrowCols {
		for i, f := range filters {
			s := t.style(t.dim)
			if i == r.filter {
				s = lipgloss.NewStyle().Foreground(t.fg).Underline(true)
			}
			tabs = append(tabs, s.Render(f))
		}
	}
	lines := []string{"  " + t.style(t.fg).Render(trunc(head, max(8, w/2))) + "  " + strings.Join(tabs, " ")}
	if len(tabs) == 0 {
		lines[0] = "  " + t.style(t.fg).Render(trunc(head, w-2))
	}
	if r.err != "" {
		lines = append(lines, "  "+t.style(t.bad).Render(r.err))
	}
	if len(vis) == 0 && r.err == "" {
		lines = append(lines, "  "+t.style(t.dim).Render("no hits"))
	}
	maxRows := min(len(vis), max(3, m.height-12))
	start := 0
	if r.cursor >= maxRows {
		start = r.cursor - maxRows + 1
	}
	for row := start; row < start+maxRows && row < len(vis); row++ {
		p := r.items[vis[row]]
		h := p.hit
		cursor, name := "  ", t.style(t.fg)
		if row == r.cursor {
			cursor, name = t.style(t.clay).Render("▸ "), lipgloss.NewStyle().Foreground(t.fg).Bold(true)
		}
		box := t.style(t.dim).Render("○ ")
		if p.selected {
			box = t.style(t.clay).Render("● ")
		}
		kind := t.style(t.dim).Render(pad(h.Type, 7))
		nameW := 28
		if w < mediumCols {
			nameW = w - 18
		}
		line := "  " + cursor + box + kind + name.Render(pad(trunc(h.Name, nameW), nameW))
		if w >= mediumCols {
			var facts []string
			if h.ArtistName != "" && h.Type != "artist" {
				facts = append(facts, h.ArtistName)
			}
			if len(h.ReleaseDate) >= 4 {
				facts = append(facts, h.ReleaseDate[:4])
			}
			if h.TrackCount > 0 {
				facts = append(facts, trackCount(h.TrackCount))
			}
			if h.DurationMS > 0 {
				facts = append(facts, format.Duration(time.Duration(h.DurationMS)*time.Millisecond))
			}
			line += "  " + t.style(t.dim).Render(trunc(strings.Join(facts, " · "), w-lipgloss.Width(line)-16))
			if h.PriorJob != nil {
				mark := "✓ in library"
				if h.PriorJob.Status != domain.JobCompleted {
					mark = "↻ downloading"
				}
				line += "  " + t.style(t.ok).Render(mark)
			}
		}
		lines = append(lines, line)
	}
	keys := "↑↓ move · space selects · enter downloads · tab filters · → artist's albums · esc closes"
	if r.artist != nil {
		keys = "↑↓ move · space selects · enter downloads · ← back · esc closes"
	}
	if w < wideCols {
		keys = "↑↓ space enter tab → esc"
	}
	lines = append(lines, "  "+t.style(t.dim).Render(keys))
	return strings.Join(lines, "\n")
}

func (m *model) helpView(w int) string {
	t := m.theme
	keys := [][2]string{
		{"enter", "search, download a pasted link, or run a /command"},
		{"↑ ↓", "history; in a list, move"},
		{"tab", "complete a /command; in search, change the filter"},
		{"ctrl+o", "show every track of the first job"},
		{"ctrl+t", "show every job in progress"},
		{"ctrl+l", "redraw"},
		{"esc", "close the panel or clear the prompt"},
		{"ctrl+c ×2", "quit; downloads keep running on the server"},
	}
	var lines []string
	for _, k := range keys {
		lines = append(lines, "  "+t.style(t.fg).Render(pad(k[0], 11))+t.style(t.dim).Render(trunc(k[1], w-15)))
	}
	lines = append(lines, "  "+t.style(t.dim).Render("commands: type / to list them · esc closes"))
	return strings.Join(lines, "\n")
}

func (m *model) prompt(w int) string {
	t := m.theme
	ph := "paste a link, type to search, / for commands"
	if w < mediumCols {
		ph = "link, search or /"
	}
	if m.panel == panelLogin {
		ph = map[string]string{"username": "Apple ID", "password": "password", "code": "code"}[m.loginSt.step]
	}
	m.input.Placeholder = ph
	m.input.SetWidth(max(4, w-8))
	prefix := t.style(t.clay).Render("> ")
	if m.busy != "" {
		prefix = t.style(t.dim).Render("… ")
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.faint).
		Width(w-2).Padding(0, 1).Render(prefix + m.input.View())
}

func (m *model) status(w int) string {
	t := m.theme
	running, queued := 0, 0
	for _, st := range m.jobs {
		if st.job.Status == domain.JobRunning {
			running++
		} else {
			queued++
		}
	}
	left := m.opts.Target
	if i := strings.Index(left, " ("); i > 0 && w < wideCols {
		left = left[:i]
	}
	left += fmt.Sprintf(" · %d running · %d queued", running, queued)
	switch m.conn.Phase {
	case client.Offline:
		left += " · offline, retrying"
	case client.Connecting:
		left += " · connecting"
	}
	if m.busy != "" {
		left += " · " + m.busy + "…"
	}
	right := "? keys · / commands · ctrl+c ×2 quits"
	if !m.quitHint.IsZero() {
		right = "press ctrl+c again to quit"
	}
	rc := t.dim
	if !m.quitHint.IsZero() {
		rc = t.bad
	}
	r := right + " "
	if w-lipgloss.Width(r) < 2+lipgloss.Width(left)+2 {
		// No room for both: the quit warning wins, else the counts.
		if !m.quitHint.IsZero() {
			return t.style(rc).Render(trunc("  "+right, w))
		}
		return t.style(t.dim).Render(trunc("  "+left, w))
	}
	return pad(t.style(t.dim).Render("  "+left), w-lipgloss.Width(r)) + t.style(rc).Render(r)
}

func trackCount(n int) string {
	if n == 1 {
		return "1 track"
	}
	return fmt.Sprintf("%d tracks", n)
}

func pad(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// trunc cuts s to w columns with an ellipsis.
func trunc(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
