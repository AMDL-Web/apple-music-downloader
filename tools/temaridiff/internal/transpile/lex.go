package transpile

import (
	"fmt"
	"strconv"
	"strings"
)

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokInt
	tokPunct
)

type token struct {
	kind   tokKind
	text   string // identifier, punctuator, or the literal's digits without suffix
	suffix string // integer literal suffix: "u8", "u32", "usize" or ""
	val    uint64 // integer literal value
	line   int
}

type comment struct {
	text string // without the leading //
	line int
}

// puncts lists multi-character punctuators before their prefixes so the
// longest match wins.
var puncts = []string{
	"<<=", ">>=",
	"->", "=>", "::", "==", "!=", "<=", ">=", "<<", ">>", "&&", "||",
	"+=", "-=", "*=", "/=", "%=", "^=", "&=", "|=", "..",
	"+", "-", "*", "/", "%", "^", "&", "|", "!", "=", "<", ">",
	"(", ")", "[", "]", "{", "}", ",", ";", ":", ".", "#",
}

func lex(src string) ([]token, []comment, error) {
	var toks []token
	var comments []comment
	line := 1
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			end := strings.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			comments = append(comments, comment{text: src[i+2 : i+end], line: line})
			i += end
		case strings.HasPrefix(src[i:], "/*"):
			return nil, nil, fmt.Errorf("line %d: block comments are not supported", line)
		case isIdentStart(c):
			j := i + 1
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
			toks = append(toks, token{kind: tokIdent, text: src[i:j], line: line})
			i = j
		case c >= '0' && c <= '9':
			tok, n, err := lexInt(src[i:])
			if err != nil {
				return nil, nil, fmt.Errorf("line %d: %v", line, err)
			}
			tok.line = line
			toks = append(toks, tok)
			i += n
		default:
			matched := false
			for _, p := range puncts {
				if strings.HasPrefix(src[i:], p) {
					toks = append(toks, token{kind: tokPunct, text: p, line: line})
					i += len(p)
					matched = true
					break
				}
			}
			if !matched {
				return nil, nil, fmt.Errorf("line %d: unexpected character %q", line, c)
			}
		}
	}
	toks = append(toks, token{kind: tokEOF, line: line})
	return toks, comments, nil
}

func lexInt(s string) (token, int, error) {
	base := 10
	i := 0
	if strings.HasPrefix(s, "0x") {
		base = 16
		i = 2
	}
	start := i
	for i < len(s) && (isDigit(s[i], base) || s[i] == '_') {
		i++
	}
	digits := strings.ReplaceAll(s[start:i], "_", "")
	if digits == "" {
		return token{}, 0, fmt.Errorf("malformed integer literal %q", s[:i])
	}
	val, err := strconv.ParseUint(digits, base, 64)
	if err != nil {
		return token{}, 0, err
	}
	j := i
	for j < len(s) && isIdentPart(s[j]) {
		j++
	}
	suffix := s[i:j]
	switch suffix {
	case "", "u8", "u32", "usize":
	default:
		return token{}, 0, fmt.Errorf("unsupported integer suffix %q", suffix)
	}
	text := digits
	if base == 16 {
		text = "0x" + digits
	}
	return token{kind: tokInt, text: text, suffix: suffix, val: val}, j, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isDigit(c byte, base int) bool {
	if c >= '0' && c <= '9' {
		return true
	}
	return base == 16 && ((c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))
}
