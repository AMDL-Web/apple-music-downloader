package transpile

import (
	"fmt"
	"math/bits"
	"sort"
	"strings"
)

// sym is a u32 expression in the emitter's simplified form. upstream's
// rounds_gen.rs is machine output full of mixed boolean-arithmetic (MBA)
// obfuscation such as ((x - ((2*x) & 2K)) + K) ^ K, which is just x. LLVM
// folds most of it for Temari; the Go compiler does not, so the translator
// folds it here with exact identities over wrapping u32 arithmetic and
// known-zero bits, and prints the smaller expression. Nothing here is an
// approximation: every rewrite holds for all inputs whose bits are allowed by
// the operands' possible-one masks, and simplify_test.go checks each rule
// against direct evaluation.
type sym struct {
	k     symKind
	c     uint32 // sConst value; constant operand of sSum, sXor and sOr; mask of sAnd; amount of sShr
	terms []term // sSum
	xs    []*sym // operands of sXor, sOr, sAndV, sMulV; xs[0] of sAnd and sShr
	code  string // sLeaf: Go code of an opaque value, parenthesized unless primary
	po    uint32 // bits that may be one
	inl   bool   // only constants, inlinable leaves and arithmetic: safe to repeat at a later point
	size  int    // node count
	key   string // structural identity
}

type symKind uint8

const (
	sConst symKind = iota
	sLeaf
	sSum  // Σ coef·term + c
	sXor  // xs[0] ^ ... ^ c
	sOr   // xs[0] | ... | c
	sAnd  // xs[0] & c
	sAndV // xs[0] & xs[1]
	sMulV // xs[0] * xs[1]
	sShr  // xs[0] >> c
)

type term struct {
	coef uint32
	x    *sym
}

const all = ^uint32(0)

func cnst(c uint32) *sym {
	return &sym{k: sConst, c: c, po: c, inl: true, size: 1, key: fmt.Sprintf("#%x", c)}
}

// leaf is an opaque value printed as code. inl marks values that read nothing
// that can change later in the function (SSA locals and parameters).
//
// Operands print in key order. Keep the key as it is unless a benchmark says
// otherwise: on amd64 the rounds' speed has moved by a third with code layout
// alone (a different operand order, or an unrelated function added to the
// binary), and this key gave the layout measured fast.
func leaf(code string, prec int, po uint32, inl bool) *sym {
	if prec < precPrimary {
		code = "(" + code + ")"
	}
	return &sym{k: sLeaf, code: code, po: po, inl: inl, size: 1, key: "L" + code + "\x00"}
}

func (s *sym) isConst() bool { return s.k == sConst }

// shlForm reports whether s is t << n, which is kept as the sum 2^n·t.
func (s *sym) shlForm() (t *sym, n int, ok bool) {
	if s.k != sSum || s.c != 0 || len(s.terms) != 1 {
		return nil, 0, false
	}
	k := s.terms[0].coef
	if k == 0 || k&(k-1) != 0 || k == 1 {
		return nil, 0, false
	}
	return s.terms[0].x, bits.TrailingZeros32(k), true
}

func finish(s *sym, ops ...*sym) *sym {
	s.inl, s.size = true, 1
	for _, o := range ops {
		s.inl = s.inl && o.inl
		s.size += o.size
	}
	return s
}

func keyOf(op string, c uint32, parts []string) string {
	return op + "(" + strings.Join(parts, ",") + fmt.Sprintf(";%x)", c)
}

// --- constructors; each returns its operands' simplest equivalent form.

func scale(x *sym, k uint32) *sym {
	switch {
	case k == 0:
		return cnst(0)
	case k == 1:
		return x
	case x.isConst():
		return cnst(x.c * k)
	case x.k == sSum:
		ts := make([]term, len(x.terms))
		for i, t := range x.terms {
			ts[i] = term{t.coef * k, t.x}
		}
		return mkSum(ts, x.c*k)
	}
	return mkSum([]term{{k, x}}, 0)
}

func (s *sym) asTerms() ([]term, uint32) {
	switch s.k {
	case sConst:
		return nil, s.c
	case sSum:
		return append([]term(nil), s.terms...), s.c
	}
	return []term{{1, s}}, 0
}

func add(x, y *sym) *sym {
	if r := mbaWhole(x, y); r != nil {
		return r
	}
	if r := mbaWhole(y, x); r != nil {
		return r
	}
	tx, cx := x.asTerms()
	ty, cy := y.asTerms()
	return mkSum(append(tx, ty...), cx+cy)
}

func sub(x, y *sym) *sym { return add(x, neg(y)) }
func neg(x *sym) *sym    { return scale(x, all) }
func not(x *sym) *sym    { return xor(x, cnst(all)) }

// mbaWhole folds a + y where y holds -2·(a & K) and a is a whole operand
// (often itself a sum that flattening would split): a - 2(a&K) == (a^K) - K.
func mbaWhole(a, y *sym) *sym {
	if a.isConst() {
		return nil
	}
	ty, cy := y.asTerms()
	for i, t := range ty {
		if t.coef != all-1 {
			continue
		}
		k, ok := andMask(t.x, a)
		if !ok {
			continue
		}
		rest := append(append([]term(nil), ty[:i]...), ty[i+1:]...)
		rest = append(rest, term{1, xor(a, cnst(k))})
		return mkSum(rest, cy-k)
	}
	return nil
}

// andMask reports whether m is a & K for some K, and returns K. Masking is
// canonical, so m is a & K exactly when masking a with the bits m may have
// builds m again.
func andMask(m, a *sym) (uint32, bool) {
	if m.isConst() || a.isConst() || m.po == 0 {
		return 0, false
	}
	return m.po, andc(a, m.po).key == m.key
}

func mkSum(ts []term, c uint32) *sym {
	r := foldSum(ts, c)
	// Linear MBA: y ^ k == y + k - 2(y & k) and y | k == y + k - (y & k).
	// Writing masked operands that way lets their terms cancel; keep the
	// result when it is smaller.
	var exp []term
	ec := c
	expanded := false
	for _, t := range ts {
		x := t.x
		if (x.k == sXor || x.k == sOr) && len(x.xs) == 1 && x.c != 0 {
			y, k := x.xs[0], x.c
			yt, yc := y.asTerms()
			for _, u := range yt {
				exp = append(exp, term{t.coef * u.coef, u.x})
			}
			ec += t.coef*yc + t.coef*k
			f := 0 - 2*t.coef
			if x.k == sOr {
				f = 0 - t.coef
			}
			exp = append(exp, term{f, andc(y, k)})
			expanded = true
			continue
		}
		exp = append(exp, t)
	}
	if expanded {
		if alt := foldSum(exp, ec); alt.size < r.size {
			return alt
		}
	}
	return r
}

func foldSum(ts []term, c uint32) *sym {
	// y ^ k == k - y when k has every bit y may have.
	var flat []term
	for _, t := range ts {
		x := t.x
		if x.k == sXor && len(x.xs) == 1 && x.xs[0].po&^x.c == 0 {
			c += t.coef * x.c
			yt, yc := x.xs[0].asTerms()
			for _, u := range yt {
				flat = append(flat, term{0 - t.coef*u.coef, u.x})
			}
			c -= t.coef * yc
			continue
		}
		flat = append(flat, t)
	}
	ts = mergeTerms(flat)
	// a·x - 2a·(x & K) == a·((x ^ K) - K)
	for changed := true; changed; {
		changed = false
	search:
		for j, tj := range ts {
			for i, ti := range ts {
				if i == j || tj.coef != 0-2*ti.coef {
					continue
				}
				k, ok := andMask(tj.x, ti.x)
				if !ok {
					continue
				}
				ts[i] = term{ti.coef, xor(ti.x, cnst(k))}
				c -= ti.coef * k
				ts = append(ts[:j], ts[j+1:]...)
				ts = mergeTerms(ts)
				changed = true
				break search
			}
			// The same with a sum: c·u - 2c·(u & K) == c·((u ^ K) - K) when the
			// sum holds every term of c·u.
			if tj.x.k == sAnd && tj.coef&1 == 0 {
				u, k := tj.x.xs[0], tj.x.c
				ut, uc := u.asTerms()
				for _, cu := range []uint32{(0 - tj.coef) / 2, (0-tj.coef)/2 + 1<<31} {
					if rest, ok := removeTerms(ts, j, ut, cu); ok {
						ts = mergeTerms(append(rest, term{cu, xor(u, cnst(k))}))
						c -= cu*k + cu*uc
						changed = true
						break search
					}
				}
			}
		}
	}
	if len(ts) == 0 {
		return cnst(c)
	}
	if len(ts) == 1 {
		t := ts[0]
		if t.coef == 1 {
			if c == 0 {
				return t.x
			}
			// No carries: x + c == x ^ c when they share no bits, and adding
			// the top bit only flips it.
			if c&t.x.po == 0 || c == 1<<31 {
				return xor(t.x, cnst(c))
			}
		} else if t.coef&(t.coef-1) == 0 && c != 0 {
			sh := mkSum([]term{t}, 0)
			if c&sh.po == 0 || c == 1<<31 {
				return xor(sh, cnst(c))
			}
		}
	} else {
		disjoint := c
		ok := true
		for _, t := range ts {
			if t.coef != 1 || disjoint&t.x.po != 0 {
				ok = false
				break
			}
			disjoint |= t.x.po
		}
		if ok {
			r := cnst(c)
			for _, t := range ts {
				r = xor(r, t.x)
			}
			return r
		}
	}
	s := &sym{k: sSum, terms: ts, c: c, po: sumPo(ts, c)}
	parts := make([]string, len(ts))
	ops := make([]*sym, len(ts))
	for i, t := range ts {
		parts[i] = fmt.Sprintf("%x*%s", t.coef, t.x.key)
		ops[i] = t.x
	}
	s.key = keyOf("+", c, parts)
	return finish(s, ops...)
}

// removeTerms returns ts without ts[skip] and without k·t for every term t
// of ut, or false when ts lacks one of them.
func removeTerms(ts []term, skip int, ut []term, k uint32) ([]term, bool) {
	if len(ut) == 0 {
		return nil, false
	}
	drop := map[int]bool{skip: true}
	for _, u := range ut {
		found := false
		for i, t := range ts {
			if !drop[i] && t.x.key == u.x.key && t.coef == k*u.coef {
				drop[i], found = true, true
				break
			}
		}
		if !found {
			return nil, false
		}
	}
	var rest []term
	for i, t := range ts {
		if !drop[i] {
			rest = append(rest, t)
		}
	}
	return rest, true
}

func mergeTerms(ts []term) []term {
	byKey := map[string]int{}
	var out []term
	for _, t := range ts {
		if i, ok := byKey[t.x.key]; ok {
			out[i].coef += t.coef
			continue
		}
		byKey[t.x.key] = len(out)
		out = append(out, t)
	}
	kept := out[:0]
	for _, t := range out {
		if t.coef != 0 {
			kept = append(kept, t)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].x.key < kept[j].x.key })
	return kept
}

// sumPo bounds the bits of Σ coef·x + c: trailing zeros common to every
// addend stay zero, and a sum of small non-negative addends stays below the
// next power of two above its largest possible value.
func sumPo(ts []term, c uint32) uint32 {
	if len(ts) == 1 && c == 0 {
		k := ts[0].coef
		if k&(k-1) == 0 {
			return ts[0].x.po << bits.TrailingZeros32(k)
		}
	}
	tz := 32
	if c != 0 {
		tz = bits.TrailingZeros32(c)
	}
	var max uint64 = uint64(c)
	small := true
	for _, t := range ts {
		z := 32
		if t.x.po != 0 {
			z = min(32, bits.TrailingZeros32(t.coef)+bits.TrailingZeros32(t.x.po))
		}
		tz = min(tz, z)
		if t.coef > 0xFFFF {
			small = false
		}
		max += uint64(t.coef) * uint64(t.x.po)
	}
	if tz >= 32 {
		return 0
	}
	po := all << tz
	if small && max < 1<<32 {
		po &= uint32(1<<bits.Len64(max) - 1)
	}
	return po
}

func xor(x, y *sym) *sym {
	var ops []*sym
	var c uint32
	var gather func(s *sym)
	gather = func(s *sym) {
		switch {
		case s.isConst():
			c ^= s.c
		case s.k == sXor:
			c ^= s.c
			ops = append(ops, s.xs...)
		default:
			// (t ^ k) << n == (t << n) ^ (k << n)
			if t, n, ok := s.shlForm(); ok && t.k == sXor && t.c != 0 {
				c ^= t.c << n
				gather(scale(withoutConst(t), 1<<n))
				return
			}
			ops = append(ops, s)
		}
	}
	gather(x)
	gather(y)
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].key < ops[j].key })
	kept := ops[:0]
	for i := 0; i < len(ops); i++ {
		if i+1 < len(ops) && ops[i].key == ops[i+1].key {
			i++ // x ^ x == 0
			continue
		}
		kept = append(kept, ops[i])
	}
	return mkXor(kept, c)
}

func withoutConst(s *sym) *sym {
	if s.k != sXor {
		return s
	}
	return mkXor(s.xs, 0)
}

func mkXor(ops []*sym, c uint32) *sym {
	if len(ops) == 0 {
		return cnst(c)
	}
	if len(ops) == 1 && c == 0 {
		return ops[0]
	}
	s := &sym{k: sXor, xs: ops, c: c, po: c}
	parts := make([]string, len(ops))
	for i, o := range ops {
		s.po |= o.po
		parts[i] = o.key
	}
	s.key = keyOf("^", c, parts)
	return finish(s, ops...)
}

func or(x, y *sym) *sym {
	var ops []*sym
	var c uint32
	for _, s := range []*sym{x, y} {
		switch {
		case s.isConst():
			c |= s.c
		case s.k == sOr:
			c |= s.c
			ops = append(ops, s.xs...)
		default:
			ops = append(ops, s)
		}
	}
	// x | c == c when every bit x may have is in c.
	kept := ops[:0]
	for _, o := range ops {
		if o.po&^c != 0 {
			kept = append(kept, o)
		}
	}
	ops = kept
	// Operands without common bits: | is ^.
	seen := c
	disjoint := true
	for _, o := range ops {
		if seen&o.po != 0 {
			disjoint = false
		}
		seen |= o.po
	}
	if disjoint {
		r := cnst(c)
		for _, o := range ops {
			r = xor(r, o)
		}
		return r
	}
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].key < ops[j].key })
	dedup := ops[:0]
	for i, o := range ops {
		if i > 0 && o.key == ops[i-1].key {
			continue
		}
		dedup = append(dedup, o)
	}
	ops = dedup
	if len(ops) == 1 && c == 0 {
		return ops[0]
	}
	s := &sym{k: sOr, xs: ops, c: c, po: c}
	parts := make([]string, len(ops))
	for i, o := range ops {
		s.po |= o.po
		parts[i] = o.key
	}
	s.key = keyOf("|", c, parts)
	return finish(s, ops...)
}

func and(x, y *sym) *sym {
	switch {
	case x.isConst():
		return andc(y, x.c)
	case y.isConst():
		return andc(x, y.c)
	case x.key == y.key:
		return x
	}
	if y.key < x.key {
		x, y = y, x
	}
	s := &sym{k: sAndV, xs: []*sym{x, y}, po: x.po & y.po, key: keyOf("&&", 0, []string{x.key, y.key})}
	return finish(s, x, y)
}

func andc(x *sym, m uint32) *sym {
	m &= x.po
	switch {
	case m == 0:
		return cnst(0)
	case m == x.po:
		return x // the mask clears nothing x may have
	case x.isConst():
		return cnst(x.c & m)
	case x.k == sAnd:
		return andc(x.xs[0], x.c&m)
	case x.k == sXor:
		// Operands and constant bits outside m do not reach the result.
		var ops []*sym
		for _, o := range x.xs {
			if o.po&m != 0 {
				ops = append(ops, o)
			}
		}
		if len(ops) < len(x.xs) || x.c&^m != 0 {
			return andc(mkXor(ops, x.c&m), m)
		}
	}
	// (t << n) & m == (t & (m >> n)) << n
	if t, n, ok := x.shlForm(); ok {
		return scale(andc(t, m>>n), 1<<n)
	}
	plain := &sym{k: sAnd, xs: []*sym{x}, c: m, po: m, key: keyOf("&", m, []string{x.key})}
	plain = finish(plain, x)
	// (a ^ b ^ k) & m == (a&m) ^ (b&m) ^ (k&m), when that is no larger.
	if x.k == sXor {
		d := cnst(x.c & m)
		for _, o := range x.xs {
			d = xor(d, andc(o, m))
		}
		if d.size < plain.size {
			return d
		}
	}
	return plain
}

func shl(x *sym, n uint32) *sym {
	if n == 0 {
		return x
	}
	return scale(x, 1<<n)
}

func shr(x *sym, n uint32) *sym {
	switch {
	case n == 0:
		return x
	case x.isConst():
		return cnst(x.c >> n)
	case x.po>>n == 0:
		return cnst(0)
	case x.k == sShr:
		if x.c+n >= 32 {
			return cnst(0)
		}
		return shr(x.xs[0], x.c+n)
	case x.k == sAnd:
		return andc(shr(x.xs[0], n), x.c>>n)
	}
	if t, k, ok := x.shlForm(); ok && uint32(k) == n {
		return andc(t, all>>n)
	}
	plain := &sym{k: sShr, xs: []*sym{x}, c: n, po: x.po >> n, key: keyOf(">>", n, []string{x.key})}
	plain = finish(plain, x)
	if x.k == sXor {
		d := cnst(x.c >> n)
		for _, o := range x.xs {
			d = xor(d, shr(o, n))
		}
		if d.size < plain.size {
			return d
		}
	}
	return plain
}

func mul(x, y *sym) *sym {
	switch {
	case x.isConst():
		return scale(y, x.c)
	case y.isConst():
		return scale(x, y.c)
	}
	if y.key < x.key {
		x, y = y, x
	}
	tz := min(32, bits.TrailingZeros32(x.po)+bits.TrailingZeros32(y.po))
	po := uint32(0)
	if tz < 32 {
		po = all << tz
	}
	s := &sym{k: sMulV, xs: []*sym{x, y}, po: po, key: keyOf("*", 0, []string{x.key, y.key})}
	return finish(s, x, y)
}

// --- evaluation, for tests.

func (s *sym) eval(env map[string]uint32) uint32 {
	switch s.k {
	case sConst:
		return s.c
	case sLeaf:
		return env[s.code]
	case sSum:
		r := s.c
		for _, t := range s.terms {
			r += t.coef * t.x.eval(env)
		}
		return r
	case sXor:
		r := s.c
		for _, o := range s.xs {
			r ^= o.eval(env)
		}
		return r
	case sOr:
		r := s.c
		for _, o := range s.xs {
			r |= o.eval(env)
		}
		return r
	case sAnd:
		return s.xs[0].eval(env) & s.c
	case sAndV:
		return s.xs[0].eval(env) & s.xs[1].eval(env)
	case sMulV:
		return s.xs[0].eval(env) * s.xs[1].eval(env)
	case sShr:
		return s.xs[0].eval(env) >> s.c
	}
	panic("eval: unknown sym kind")
}

// --- printing.

// Go operator precedence levels used below.
const (
	precAdd     = 4 // + - | ^
	precMul     = 5 // * << >> &
	precPrimary = 6
)

func constCode(c uint32) string {
	if c >= 0x100 {
		return fmt.Sprintf("0x%X", c)
	}
	return fmt.Sprintf("%d", c)
}

// render prints s as Go code and returns its top-level binary operator, ""
// for a primary expression.
func (s *sym) render() (string, string) { return (*printer)(nil).render(s) }

// printer prints syms. names maps the key of an expression that a Go local
// already holds to that local, so a repeated expression is read, not
// recomputed; only expressions of stable values belong there.
type printer struct {
	names map[string]string
}

func (p *printer) render(s *sym) (string, string) {
	if p != nil && s.k != sLeaf && s.k != sConst {
		if n, ok := p.names[s.key]; ok {
			return n, ""
		}
	}
	switch s.k {
	case sConst:
		return constCode(s.c), ""
	case sLeaf:
		return s.code, ""
	case sXor, sOr:
		op := "^"
		if s.k == sOr {
			op = "|"
		}
		parts := make([]string, 0, len(s.xs)+1)
		for _, o := range s.xs {
			parts = append(parts, p.operand(o, op))
		}
		if s.c != 0 {
			parts = append(parts, constCode(s.c))
		}
		return strings.Join(parts, " "+op+" "), op
	case sAnd:
		return p.operand(s.xs[0], "&") + " & " + constCode(s.c), "&"
	case sAndV:
		return p.operand(s.xs[0], "&") + " & " + p.operand(s.xs[1], "&"), "&"
	case sMulV:
		return p.operand(s.xs[0], "*") + " * " + p.operand(s.xs[1], "*"), "*"
	case sShr:
		return p.operand(s.xs[0], ">>") + " >> " + fmt.Sprint(s.c), ">>"
	case sSum:
		return p.renderSum(s)
	}
	panic("render: unknown sym kind")
}

func opPrec(op string) int {
	switch op {
	case "":
		return precPrimary
	case "*", "<<", ">>", "&":
		return precMul
	}
	return precAdd
}

// operand prints o as an operand of op, in parentheses unless it binds
// tighter than op or is the same associative operator.
func (p *printer) operand(o *sym, op string) string {
	code, top := p.render(o)
	if top == "" || (top == op && (op == "^" || op == "|" || op == "&" || op == "+" || op == "*")) || opPrec(top) > opPrec(op) {
		return code
	}
	return "(" + code + ")"
}

func (p *printer) renderSum(s *sym) (string, string) {
	if t, n, ok := s.shlForm(); ok {
		return p.operand(t, "<<") + " << " + fmt.Sprint(n), "<<"
	}
	type part struct {
		neg  bool
		code string
	}
	var pos, negs []part
	for _, t := range s.terms {
		k, isNeg := t.coef, false
		if k >= 1<<31 {
			k, isNeg = -k, true
		}
		var code string
		switch {
		case k == 1:
			code = p.operand(t.x, "+")
			if isNeg {
				code = p.operand(t.x, "-")
			}
		case k&(k-1) == 0:
			code = p.operand(t.x, "<<") + " << " + fmt.Sprint(bits.TrailingZeros32(k))
			if isNeg {
				code = "(" + code + ")"
			}
		default:
			code = constCode(k) + " * " + p.operand(t.x, "*")
			if isNeg {
				code = "(" + code + ")"
			}
		}
		if isNeg {
			negs = append(negs, part{true, code})
		} else {
			pos = append(pos, part{false, code})
		}
	}
	var b strings.Builder
	switch {
	case len(pos) > 0:
		b.WriteString(pos[0].code)
		for _, p := range pos[1:] {
			b.WriteString(" + " + p.code)
		}
	case s.c != 0 && s.c < 1<<31:
		b.WriteString(constCode(s.c))
	default:
		b.WriteString("0")
	}
	for _, p := range negs {
		b.WriteString(" - " + p.code)
	}
	if s.c != 0 && !(len(pos) == 0 && s.c < 1<<31) {
		if s.c < 1<<31 {
			b.WriteString(" + " + constCode(s.c))
		} else {
			b.WriteString(" - " + constCode(-s.c))
		}
	}
	return b.String(), "+"
}
