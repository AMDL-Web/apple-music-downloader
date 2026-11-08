package transpile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type typeExpr struct {
	prim  string // "u8", "u32", "usize", "bool" or a named type
	ref   bool
	mut   bool
	slice bool // [elem]
	array bool // [elem; n]
	tuple []*typeExpr
	elem  *typeExpr
	n     uint64
}

type expr interface{}

type (
	intLit struct {
		text   string
		suffix string
		val    uint64
	}
	identExpr struct{ name string }
	fieldExpr struct {
		x    expr
		name string
	}
	indexExpr struct{ x, idx expr }
	callExpr  struct {
		fn   string
		args []expr
	}
	methodExpr struct {
		recv   expr
		method string
		args   []expr
	}
	castExpr struct {
		x  expr
		to string
	}
	unaryExpr struct {
		op string
		x  expr
	}
	binaryExpr struct {
		op   string
		x, y expr
	}
	tupleExpr  struct{ elems []expr }
	arrayExpr  struct{ elems []expr }
	repeatExpr struct {
		elem expr
		n    uint64
	}
	structExpr struct {
		name   string
		fields []structField
	}
)

type structField struct {
	name string
	val  expr
}

type stmt interface{}

type (
	letStmt struct {
		names []string
		tuple bool
		init  expr
	}
	assignStmt struct {
		lhs expr
		op  string // "=" or a compound operator such as "^="
		rhs expr
	}
	ifStmt struct {
		cond      expr
		then, els []stmt
	}
	forStmt struct {
		names []string
		iter  expr
		body  []stmt
	}
	commentStmt struct{ text string }
	returnStmt  struct{ x expr }
)

type param struct {
	name string
	typ  *typeExpr
}

type fieldDecl struct {
	comments []string
	name     string
	typ      *typeExpr
}

type item interface{}

type (
	constItem struct {
		comments []string
		name     string
		typ      *typeExpr
		val      *intLit
	}
	typeItem struct {
		comments []string
		name     string
		typ      *typeExpr
	}
	structItem struct {
		comments []string
		name     string
		fields   []fieldDecl
	}
	fnItem struct {
		comments []string
		name     string
		params   []param
		ret      *typeExpr
		body     []stmt
		digest   string // sha256 of the item's tokens, for pinning hand-ported items
	}
)

type parser struct {
	toks     []token
	comments []comment
	pos      int
	cpos     int
	opaque   map[string]string
}

// parseFile parses src. Functions named in opaque keep their signature, but
// their bodies are skipped unparsed: they are ported by hand.
func parseFile(src string, opaque map[string]string) ([]item, error) {
	toks, comments, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks, comments: comments, opaque: opaque}
	var items []item
	for {
		p.skipAttributes()
		if p.peek().kind == tokEOF {
			break
		}
		it, err := p.item()
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", p.peek().line, err)
		}
		items = append(items, it)
	}
	return items, nil
}

func (p *parser) peek() token { return p.toks[p.pos] }
func (p *parser) peekAt(n int) token {
	if p.pos+n < len(p.toks) {
		return p.toks[p.pos+n]
	}
	return p.toks[len(p.toks)-1]
}
func (p *parser) next() token {
	t := p.toks[p.pos]
	if t.kind != tokEOF {
		p.pos++
	}
	return t
}

func (p *parser) is(text string) bool {
	t := p.peek()
	return (t.kind == tokPunct || t.kind == tokIdent) && t.text == text
}

func (p *parser) accept(text string) bool {
	if p.is(text) {
		p.next()
		return true
	}
	return false
}

func (p *parser) expect(text string) error {
	if !p.accept(text) {
		return fmt.Errorf("expected %q, found %q", text, p.peek().text)
	}
	return nil
}

func (p *parser) ident() (string, error) {
	t := p.peek()
	if t.kind != tokIdent {
		return "", fmt.Errorf("expected identifier, found %q", t.text)
	}
	p.next()
	return t.text, nil
}

// takeComments returns the comments that start before line, in order.
func (p *parser) takeComments(line int) []string {
	var out []string
	for p.cpos < len(p.comments) && p.comments[p.cpos].line < line {
		out = append(out, p.comments[p.cpos].text)
		p.cpos++
	}
	return out
}

func (p *parser) skipAttributes() {
	for p.is("#") {
		p.next()
		p.accept("!")
		depth := 0
		for {
			t := p.next()
			if t.kind == tokEOF {
				return
			}
			if t.text == "[" {
				depth++
			} else if t.text == "]" {
				depth--
				if depth == 0 {
					break
				}
			}
		}
	}
}

func (p *parser) item() (item, error) {
	start := p.pos
	comments := p.takeComments(p.peek().line)
	p.accept("pub")
	switch {
	case p.accept("const"):
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		typ, err := p.typ()
		if err != nil {
			return nil, err
		}
		if err := p.expect("="); err != nil {
			return nil, err
		}
		t := p.next()
		if t.kind != tokInt {
			return nil, fmt.Errorf("const %s: only integer literals are supported", name)
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		return &constItem{comments: comments, name: name, typ: typ, val: &intLit{text: t.text, suffix: t.suffix, val: t.val}}, nil
	case p.accept("type"):
		name, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect("="); err != nil {
			return nil, err
		}
		typ, err := p.typ()
		if err != nil {
			return nil, err
		}
		return &typeItem{comments: comments, name: name, typ: typ}, p.expect(";")
	case p.accept("struct"):
		return p.structItem(comments)
	case p.accept("fn"):
		fn, err := p.fnItem(comments)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		for _, t := range p.toks[start:p.pos] {
			h.Write([]byte(t.text + t.suffix + "\x00"))
		}
		fn.digest = hex.EncodeToString(h.Sum(nil))
		return fn, nil
	}
	return nil, fmt.Errorf("unsupported item starting with %q", p.peek().text)
}

func (p *parser) structItem(comments []string) (item, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	s := &structItem{comments: comments, name: name}
	for !p.is("}") {
		fc := p.takeComments(p.peek().line)
		p.accept("pub")
		fname, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		ftyp, err := p.typ()
		if err != nil {
			return nil, err
		}
		s.fields = append(s.fields, fieldDecl{comments: fc, name: fname, typ: ftyp})
		if !p.accept(",") {
			break
		}
	}
	return s, p.expect("}")
}

func (p *parser) typ() (*typeExpr, error) {
	if p.accept("&") {
		mut := p.accept("mut")
		inner, err := p.typ()
		if err != nil {
			return nil, err
		}
		cp := *inner
		cp.ref, cp.mut = true, mut
		if inner.ref {
			return nil, fmt.Errorf("nested references are not supported")
		}
		return &cp, nil
	}
	if p.accept("[") {
		elem, err := p.typ()
		if err != nil {
			return nil, err
		}
		if p.accept(";") {
			t := p.next()
			if t.kind != tokInt {
				return nil, fmt.Errorf("array length must be an integer literal")
			}
			return &typeExpr{array: true, elem: elem, n: t.val}, p.expect("]")
		}
		return &typeExpr{slice: true, elem: elem}, p.expect("]")
	}
	if p.accept("(") {
		var elems []*typeExpr
		for !p.is(")") {
			e, err := p.typ()
			if err != nil {
				return nil, err
			}
			elems = append(elems, e)
			if !p.accept(",") {
				break
			}
		}
		if len(elems) < 2 {
			return nil, fmt.Errorf("only tuple types with two or more elements are supported")
		}
		return &typeExpr{tuple: elems}, p.expect(")")
	}
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	return &typeExpr{prim: name}, nil
}

func (p *parser) fnItem(comments []string) (*fnItem, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	fn := &fnItem{comments: comments, name: name}
	if err := p.expect("("); err != nil {
		return nil, err
	}
	for !p.is(")") {
		pname, err := p.ident()
		if err != nil {
			return nil, err
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		ptyp, err := p.typ()
		if err != nil {
			return nil, err
		}
		fn.params = append(fn.params, param{name: pname, typ: ptyp})
		if !p.accept(",") {
			break
		}
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	if p.accept("->") {
		if fn.ret, err = p.typ(); err != nil {
			return nil, err
		}
	}
	if _, ok := p.opaque[name]; ok {
		return fn, p.skipBlock()
	}
	fn.body, err = p.block(true)
	return fn, err
}

func (p *parser) skipBlock() error {
	if !p.is("{") {
		return fmt.Errorf("expected a function body")
	}
	depth := 0
	for {
		t := p.next()
		switch {
		case t.kind == tokEOF:
			return fmt.Errorf("unterminated function body")
		case t.kind == tokPunct && t.text == "{":
			depth++
		case t.kind == tokPunct && t.text == "}":
			depth--
			if depth == 0 {
				p.takeComments(t.line + 1)
				return nil
			}
		}
	}
}

// block parses { stmts }. A trailing expression without a semicolon becomes a
// returnStmt when tail is set.
func (p *parser) block(tail bool) ([]stmt, error) {
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	var out []stmt
	for {
		for _, c := range p.takeComments(p.peek().line) {
			out = append(out, &commentStmt{text: c})
		}
		if p.accept("}") {
			return out, nil
		}
		line := p.peek().line
		s, isTail, err := p.stmt()
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if isTail {
			if !tail {
				return nil, fmt.Errorf("line %d: tail expression in a nested block", line)
			}
			out = append(out, s)
			for _, c := range p.takeComments(p.peek().line) {
				out = append(out, &commentStmt{text: c})
			}
			return out, p.expect("}")
		}
		out = append(out, s)
	}
}

func (p *parser) stmt() (stmt, bool, error) {
	switch {
	case p.accept("let"):
		p.accept("mut")
		s := &letStmt{}
		if p.accept("(") {
			s.tuple = true
			for !p.is(")") {
				n, err := p.ident()
				if err != nil {
					return nil, false, err
				}
				s.names = append(s.names, n)
				if !p.accept(",") {
					break
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, false, err
			}
		} else {
			n, err := p.ident()
			if err != nil {
				return nil, false, err
			}
			s.names = []string{n}
		}
		if p.is(":") {
			return nil, false, fmt.Errorf("typed let bindings are not supported")
		}
		if err := p.expect("="); err != nil {
			return nil, false, err
		}
		init, err := p.expr(0, false)
		if err != nil {
			return nil, false, err
		}
		s.init = init
		return s, false, p.expect(";")
	case p.accept("if"):
		cond, err := p.expr(0, true)
		if err != nil {
			return nil, false, err
		}
		then, err := p.block(false)
		if err != nil {
			return nil, false, err
		}
		s := &ifStmt{cond: cond, then: then}
		if p.accept("else") {
			if p.is("if") {
				return nil, false, fmt.Errorf("else if is not supported")
			}
			if s.els, err = p.block(false); err != nil {
				return nil, false, err
			}
		}
		return s, false, nil
	case p.accept("for"):
		if err := p.expect("&"); err != nil {
			return nil, false, err
		}
		if err := p.expect("("); err != nil {
			return nil, false, err
		}
		s := &forStmt{}
		for !p.is(")") {
			n, err := p.ident()
			if err != nil {
				return nil, false, err
			}
			s.names = append(s.names, n)
			if !p.accept(",") {
				break
			}
		}
		if err := p.expect(")"); err != nil {
			return nil, false, err
		}
		if err := p.expect("in"); err != nil {
			return nil, false, err
		}
		iter, err := p.expr(0, true)
		if err != nil {
			return nil, false, err
		}
		s.iter = iter
		s.body, err = p.block(false)
		return s, false, err
	}
	lhs, err := p.expr(0, false)
	if err != nil {
		return nil, false, err
	}
	if p.is("}") {
		return &returnStmt{x: lhs}, true, nil
	}
	t := p.peek()
	if t.kind == tokPunct && (t.text == "=" || (strings.HasSuffix(t.text, "=") && !isComparison(t.text))) {
		p.next()
		rhs, err := p.expr(0, false)
		if err != nil {
			return nil, false, err
		}
		return &assignStmt{lhs: lhs, op: t.text, rhs: rhs}, false, p.expect(";")
	}
	return nil, false, fmt.Errorf("unsupported statement near %q", t.text)
}

func isComparison(op string) bool {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=":
		return true
	}
	return false
}

// Binding powers follow the Rust reference's operator precedence table.
var binaryBP = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3,
	"|": 4, "^": 5, "&": 6, "<<": 7, ">>": 7,
	"+": 8, "-": 8, "*": 9, "/": 9, "%": 9,
}

const (
	bpCast  = 10
	bpUnary = 11
)

// expr parses an expression whose operators all bind tighter than minBP.
// noStruct disables struct literals, as Rust does in if and for heads.
func (p *parser) expr(minBP int, noStruct bool) (expr, error) {
	lhs, err := p.prefix(noStruct)
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		switch {
		case t.kind == tokPunct && t.text == ".":
			p.next()
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			if p.accept("(") {
				args, err := p.args(")")
				if err != nil {
					return nil, err
				}
				lhs = &methodExpr{recv: lhs, method: name, args: args}
			} else {
				lhs = &fieldExpr{x: lhs, name: name}
			}
		case t.kind == tokPunct && t.text == "[":
			p.next()
			idx, err := p.expr(0, false)
			if err != nil {
				return nil, err
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			lhs = &indexExpr{x: lhs, idx: idx}
		case t.kind == tokIdent && t.text == "as":
			if bpCast <= minBP {
				return lhs, nil
			}
			p.next()
			to, err := p.ident()
			if err != nil {
				return nil, err
			}
			lhs = &castExpr{x: lhs, to: to}
		case t.kind == tokPunct && binaryBP[t.text] > 0:
			bp := binaryBP[t.text]
			if bp <= minBP {
				return lhs, nil
			}
			p.next()
			rhs, err := p.expr(bp, noStruct)
			if err != nil {
				return nil, err
			}
			lhs = &binaryExpr{op: t.text, x: lhs, y: rhs}
		default:
			return lhs, nil
		}
	}
}

func (p *parser) args(closer string) ([]expr, error) {
	var out []expr
	for !p.is(closer) {
		e, err := p.expr(0, false)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if !p.accept(",") {
			break
		}
	}
	return out, p.expect(closer)
}

func (p *parser) prefix(noStruct bool) (expr, error) {
	t := p.next()
	switch {
	case t.kind == tokInt:
		return &intLit{text: t.text, suffix: t.suffix, val: t.val}, nil
	case t.kind == tokPunct && (t.text == "!" || t.text == "-"):
		x, err := p.expr(bpUnary-1, noStruct)
		if err != nil {
			return nil, err
		}
		return &unaryExpr{op: t.text, x: x}, nil
	case t.kind == tokPunct && t.text == "(":
		first, err := p.expr(0, false)
		if err != nil {
			return nil, err
		}
		if p.accept(")") {
			return first, nil
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
		rest, err := p.args(")")
		if err != nil {
			return nil, err
		}
		return &tupleExpr{elems: append([]expr{first}, rest...)}, nil
	case t.kind == tokPunct && t.text == "[":
		first, err := p.expr(0, false)
		if err != nil {
			return nil, err
		}
		if p.accept(";") {
			n := p.next()
			if n.kind != tokInt {
				return nil, fmt.Errorf("array repeat count must be an integer literal")
			}
			return &repeatExpr{elem: first, n: n.val}, p.expect("]")
		}
		if p.accept("]") {
			return &arrayExpr{elems: []expr{first}}, nil
		}
		if err := p.expect(","); err != nil {
			return nil, err
		}
		rest, err := p.args("]")
		if err != nil {
			return nil, err
		}
		return &arrayExpr{elems: append([]expr{first}, rest...)}, nil
	case t.kind == tokIdent:
		if p.is("(") {
			p.next()
			args, err := p.args(")")
			if err != nil {
				return nil, err
			}
			return &callExpr{fn: t.text, args: args}, nil
		}
		if !noStruct && p.is("{") && t.text[0] >= 'A' && t.text[0] <= 'Z' {
			return p.structLit(t.text)
		}
		if p.is("!") && p.peekAt(1).text == "(" {
			return nil, fmt.Errorf("macro %s! is not supported", t.text)
		}
		return &identExpr{name: t.text}, nil
	}
	return nil, fmt.Errorf("unexpected %q in expression", t.text)
}

func (p *parser) structLit(name string) (expr, error) {
	p.next() // {
	s := &structExpr{name: name}
	for !p.is("}") {
		fname, err := p.ident()
		if err != nil {
			return nil, err
		}
		var val expr = &identExpr{name: fname}
		if p.accept(":") {
			if val, err = p.expr(0, false); err != nil {
				return nil, err
			}
		}
		s.fields = append(s.fields, structField{name: fname, val: val})
		if !p.accept(",") {
			break
		}
	}
	return s, p.expect("}")
}
