package media

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/wrapper"
)

const appleLyricsNamespace = "http://music.apple.com/lyric-ttml-internal"

// trackLyrics is one track's lyrics in the forms its outputs take: lrc for
// the tag and the .lrc sidecar, ttml for the .ttml sidecar. ttml is empty
// when the wrapper only had plain text.
type trackLyrics struct {
	lrc  string
	ttml string
}

// sidecar is the content of the lyrics sidecar in format; "" writes none.
func (l trackLyrics) sidecar(format string) string {
	if format == "ttml" {
		return l.ttml
	}
	return l.lrc
}

// lyricsNotFoundError is wrapper-lite's answer for a song Apple serves no
// lyrics for. Asking again cannot change it.
type lyricsNotFoundError struct{ error }

func (lyricsNotFoundError) NonRetryable() bool { return true }

func (e lyricsNotFoundError) Unwrap() error { return e.error }

func isLyricsNotFound(err error) bool {
	var reply *wrapper.ReplyError
	// Code is the envelope's status; an HTTP 404 with no envelope is an
	// unknown route, which means the wrapper is not wrapper-lite.
	return errors.As(err, &reply) && reply.Code == http.StatusNotFound
}

// fetchLyricsDocument asks the wrapper for the word-timed document, which
// also carries line timing, the translation and the romanization. A song
// without synced lyrics has no such document, and only the line-timed
// request (syllable=0) falls back to Apple's plain text, so a miss asks once
// more that way.
func fetchLyricsDocument(ctx context.Context, media mediaBackend, songID string, cfg config.Config) (string, error) {
	doc, err := media.lyrics(ctx, songID, lyricsRequest(cfg, true))
	if isLyricsNotFound(err) {
		doc, err = media.lyrics(ctx, songID, lyricsRequest(cfg, false))
	}
	if isLyricsNotFound(err) {
		return "", lyricsNotFoundError{err}
	}
	return doc, err
}

// fetchLyrics fetches and converts the lyrics and records lyrics_status. A
// failure only drops the lyrics; the error it returns is the job's
// cancellation.
func (r *trackRun) fetchLyrics(ctx context.Context) error {
	d, item := r.d, &r.item
	cfg := d.cfg.Download
	if !(cfg.EmbedLyrics || len(cfg.LyricsFiles) > 0) || !r.song.HasLyrics {
		if r.song.HasLyrics {
			item.LyricsStatus = domain.LyricsDisabled
		} else {
			item.LyricsStatus = domain.LyricsNone
		}
		_ = r.reporter.UpdateItem(ctx, item)
		return nil
	}
	attempts := clampAttempts(r.maxAttempts())
	doc, lyricsAttempts, err := retryValue(ctx, r.maxAttempts(), retryBackoff, func(attempt int) (string, error) {
		d.setItemAttempt(ctx, r.reporter, item, "lyrics", attempt, attempts, fmt.Sprintf("Fetching lyrics (%d/%d)", attempt, attempts))
		return fetchLyricsDocument(ctx, r.media, r.song.ID, d.cfg)
	}, func(failure retryFailure) {
		d.setRetryFailure(ctx, r.reporter, item, "lyrics", "lyrics", failure)
		d.emitRetryEvent(ctx, r.reporter, r.job.ID, item, "lyrics", "", failure)
	})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		item.LyricsStatus = domain.LyricsFailed
		if errors.As(err, new(lyricsNotFoundError)) {
			item.StatusMessage = "The wrapper found no lyrics for this track; continuing without lyrics"
		} else {
			item.StatusMessage = "Lyrics fetch retries exhausted; continuing without embedded lyrics: " + err.Error()
		}
		_ = r.reporter.UpdateItem(ctx, item)
		return nil
	}
	lrc, convertErr := lyricsLRC(doc, cfg.LyricsTiming == "word", cfg.LyricsExtras)
	switch {
	case convertErr != nil:
		item.LyricsStatus = domain.LyricsFailed
		item.StatusMessage = "Lyrics conversion failed; continuing without embedded lyrics: " + convertErr.Error()
		_ = r.reporter.UpdateItem(ctx, item)
	case lrc == "":
		item.LyricsStatus = domain.LyricsFailed
		item.StatusMessage = "Lyrics fetch returned an empty document; continuing without embedded lyrics"
		_ = r.reporter.UpdateItem(ctx, item)
	default:
		r.lyrics = trackLyrics{lrc: lrc}
		if isTTML(doc) {
			r.lyrics.ttml = doc
		}
		item.LyricsStatus = domain.LyricsFetched
		_ = r.reporter.UpdateItem(ctx, item)
		if lyricsAttempts > 1 {
			d.emitRecoveredEvent(ctx, r.reporter, r.job.ID, item, "lyrics", "", lyricsAttempts)
		}
	}
	return nil
}

func isTTML(doc string) bool {
	return strings.HasPrefix(strings.TrimSpace(doc), "<")
}

// lyricsLRC renders the wrapper's lyrics document as LRC. Each line is
// stamped with its start; with word set, a line that has per-word times
// becomes enhanced LRC instead. extras ("translation", "pronunciation") add
// the line's translation and romanization (Apple's transliteration) under it
// with the same stamp, unless they read the same as the line. A document
// that is not TTML is Apple's plain text and passes through.
func lyricsLRC(doc string, word bool, extras []string) (string, error) {
	if !isTTML(doc) {
		return strings.TrimSpace(doc), nil
	}
	root, err := parseLyricsXML(doc)
	if err != nil {
		return "", fmt.Errorf("parse lyrics TTML: %w", err)
	}
	if root == nil {
		return "", fmt.Errorf("parse lyrics TTML: empty document")
	}
	if strings.EqualFold(attr(root, "itunes:timing", "timing"), "none") {
		return plainTextLyrics(root), nil
	}
	var translations, romanizations map[string]*lyricNode
	if slices.Contains(extras, "translation") {
		translations = localizedLines(root, "translations")
	}
	if slices.Contains(extras, "pronunciation") {
		romanizations = localizedLines(root, "transliterations")
	}
	var lines []string
	for _, p := range lyricParagraphs(root) {
		text := lineText(p)
		if text == "" {
			continue
		}
		begin := attr(p, "begin")
		if begin == "" {
			begin = firstSpanBegin(p)
		}
		if begin == "" {
			return "", fmt.Errorf("no synchronised lyrics")
		}
		stamp, err := lrcTime(begin)
		if err != nil {
			return "", err
		}
		prefix := "[" + stamp + "]"
		line, err := lrcLine(p, word)
		if err != nil {
			return "", err
		}
		lines = append(lines, prefix+line)
		key := attr(p, "itunes:key", "key")
		if translation := lineText(translations[key]); translation != "" && !strings.EqualFold(translation, text) {
			lines = append(lines, prefix+translation)
		}
		if romanized := lineText(romanizations[key]); romanized != "" && !strings.EqualFold(romanized, text) {
			line, err := lrcLine(romanizations[key], word)
			if err != nil {
				return "", err
			}
			lines = append(lines, prefix+line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// lrcLine renders a <p> or a localized <text> without its line stamp: its
// whole text, or with word set and timed spans inside it, each span's text
// after its start time and the last span's end closing the line. Untimed
// spans, such as Apple's background vocals (ttm:role="x-bg"), only group the
// timed spans inside them.
func lrcLine(node *lyricNode, word bool) (string, error) {
	if !word || firstSpanBegin(node) == "" {
		return lineText(node), nil
	}
	var b strings.Builder
	end := ""
	var walk func(*lyricNode) error
	walk = func(n *lyricNode) error {
		for _, content := range n.content {
			child := content.node
			if child == nil {
				b.WriteString(content.text)
				continue
			}
			begin := attr(child, "begin")
			if child.tag != "span" || begin == "" {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			stamp, err := lrcTime(begin)
			if err != nil {
				return err
			}
			b.WriteString("<" + stamp + ">" + preferredNodeText(child))
			if e := attr(child, "end"); e != "" {
				end = e
			}
		}
		return nil
	}
	if err := walk(node); err != nil {
		return "", err
	}
	line := collapseSpace(b.String())
	if end != "" {
		stamp, err := lrcTime(end)
		if err != nil {
			return "", err
		}
		line += "<" + stamp + ">"
	}
	return line, nil
}

// lineText is a node's text as one line, nested spans included. Apple
// separates romanized words with runs of spaces, so whitespace collapses.
func lineText(node *lyricNode) string {
	if node == nil {
		return ""
	}
	return collapseSpace(preferredNodeText(node))
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func firstSpanBegin(node *lyricNode) string {
	for _, span := range findNodes(node, "span") {
		if begin := attr(span, "begin"); begin != "" {
			return begin
		}
	}
	return ""
}

// localizedLines maps each line key to its <text> under the head's
// <translations> or <transliterations>. The wrapper sends one language of
// each; should there be more, the first wins.
func localizedLines(root *lyricNode, section string) map[string]*lyricNode {
	lines := map[string]*lyricNode{}
	for _, group := range findNodes(root, section) {
		for _, text := range findNodes(group, "text") {
			if key := attr(text, "for"); key != "" && lines[key] == nil {
				lines[key] = text
			}
		}
	}
	return lines
}

type lyricNode struct {
	tag      string
	attrs    map[string]string
	content  []lyricContent
	children []*lyricNode
}

type lyricContent struct {
	text string
	node *lyricNode
}

func parseLyricsXML(raw string) (*lyricNode, error) {
	decoder := xml.NewDecoder(strings.NewReader(raw))
	var stack []*lyricNode
	var root *lyricNode
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch tok := token.(type) {
		case xml.StartElement:
			node := &lyricNode{tag: tok.Name.Local, attrs: map[string]string{}}
			for _, xmlAttr := range tok.Attr {
				name := xmlAttr.Name.Local
				if xmlAttr.Name.Space != "" {
					name = xmlAttr.Name.Space + ":" + xmlAttr.Name.Local
				}
				node.attrs[name] = xmlAttr.Value
			}
			if len(stack) == 0 {
				root = node
			} else {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
				parent.content = append(parent.content, lyricContent{node: node})
			}
			stack = append(stack, node)
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].content = append(stack[len(stack)-1].content, lyricContent{text: string(tok)})
			}
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed XML elements")
	}
	return root, nil
}

func plainTextLyrics(root *lyricNode) string {
	var lines []string
	for _, p := range findNodes(root, "p") {
		if text := lineText(p); text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

func lyricParagraphs(root *lyricNode) []*lyricNode {
	var out []*lyricNode
	var walk func(*lyricNode, bool)
	walk = func(node *lyricNode, inBody bool) {
		if node == nil {
			return
		}
		if node.tag == "body" {
			inBody = true
		}
		if inBody && node.tag == "p" {
			out = append(out, node)
			return
		}
		for _, child := range node.children {
			walk(child, inBody)
		}
	}
	walk(root, false)
	return out
}

func findNodes(root *lyricNode, tag string) []*lyricNode {
	var out []*lyricNode
	var walk func(*lyricNode)
	walk = func(node *lyricNode) {
		if node == nil {
			return
		}
		if node.tag == tag {
			out = append(out, node)
		}
		for _, child := range node.children {
			walk(child)
		}
	}
	walk(root)
	return out
}

func preferredNodeText(node *lyricNode) string {
	if text := attr(node, "text"); text != "" {
		return text
	}
	return nodeText(node)
}

func nodeText(node *lyricNode) string {
	var b strings.Builder
	var write func(*lyricNode)
	write = func(n *lyricNode) {
		if n == nil {
			return
		}
		for _, content := range n.content {
			if content.node != nil {
				write(content.node)
			} else {
				b.WriteString(content.text)
			}
		}
	}
	write(node)
	return strings.TrimSpace(b.String())
}

func attr(node *lyricNode, names ...string) string {
	if node == nil {
		return ""
	}
	for _, name := range names {
		if value, ok := node.attrs[name]; ok {
			return value
		}
		if strings.HasPrefix(name, "itunes:") {
			expandedName := appleLyricsNamespace + ":" + strings.TrimPrefix(name, "itunes:")
			if value, ok := node.attrs[expandedName]; ok {
				return value
			}
		}
	}
	return ""
}

func lrcTime(v string) (string, error) {
	if !strings.Contains(v, ".") {
		v += ".000"
	}
	parts := strings.Split(v, ":")
	h, m := 0, 0
	secPart := parts[len(parts)-1]
	if len(parts) == 3 {
		var err error
		h, err = strconv.Atoi(parts[0])
		if err != nil {
			return "", err
		}
		m, err = strconv.Atoi(parts[1])
		if err != nil {
			return "", err
		}
	} else if len(parts) == 2 {
		var err error
		m, err = strconv.Atoi(parts[0])
		if err != nil {
			return "", err
		}
	} else if len(parts) > 3 {
		return "", fmt.Errorf("invalid lyrics timestamp %q", v)
	}
	sm := strings.SplitN(secPart, ".", 2)
	s, err := strconv.Atoi(sm[0])
	if err != nil {
		return "", err
	}
	ms := 0
	if len(sm) == 2 {
		ms, err = strconv.Atoi(rightPad(sm[1], 3)[:3])
		if err != nil {
			return "", err
		}
	}
	totalM := h*60 + m
	return left2(totalM) + ":" + left2(s) + "." + left2(ms/10), nil
}

func left2(i int) string {
	if i < 10 {
		return "0" + strconv.Itoa(i)
	}
	return strconv.Itoa(i)
}

func rightPad(s string, n int) string {
	for len(s) < n {
		s += "0"
	}
	if len(s) > n {
		return s[:n]
	}
	return s
}
