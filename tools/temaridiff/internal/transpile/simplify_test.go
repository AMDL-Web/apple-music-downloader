package transpile

import (
	"math/rand/v2"
	"testing"
)

// symLeaves are the free variables of the random expressions, with the bits
// each may have; the masks feed the known-bits rules.
var symLeaves = []struct {
	name string
	po   uint32
}{{"a", all}, {"b", 0xFF}, {"c", 0xFF00}, {"d", 0xFFFF0000}, {"e", 0x1FE}}

type symCase struct {
	s    *sym
	eval func(env map[string]uint32) uint32
}

// genSym builds a random expression with the emitter's constructors next to
// a direct evaluation of the same operations. Some shapes are the MBA
// patterns upstream uses, so the folding rules fire often.
func genSym(r *rand.Rand, depth int) symCase {
	k := func() uint32 {
		switch r.IntN(4) {
		case 0:
			return uint32(r.IntN(256))
		case 1:
			return r.Uint32() &^ uint32(r.IntN(1<<16))
		}
		return r.Uint32()
	}
	if depth == 0 || r.IntN(5) == 0 {
		if r.IntN(3) == 0 {
			c := k()
			return symCase{cnst(c), func(map[string]uint32) uint32 { return c }}
		}
		l := symLeaves[r.IntN(len(symLeaves))]
		return symCase{leaf(l.name, precPrimary, l.po, true), func(env map[string]uint32) uint32 { return env[l.name] }}
	}
	x := genSym(r, depth-1)
	y := genSym(r, depth-1)
	switch r.IntN(16) {
	case 0:
		return symCase{add(x.s, y.s), func(e map[string]uint32) uint32 { return x.eval(e) + y.eval(e) }}
	case 1:
		return symCase{sub(x.s, y.s), func(e map[string]uint32) uint32 { return x.eval(e) - y.eval(e) }}
	case 2:
		return symCase{xor(x.s, y.s), func(e map[string]uint32) uint32 { return x.eval(e) ^ y.eval(e) }}
	case 3:
		return symCase{or(x.s, y.s), func(e map[string]uint32) uint32 { return x.eval(e) | y.eval(e) }}
	case 4:
		return symCase{and(x.s, y.s), func(e map[string]uint32) uint32 { return x.eval(e) & y.eval(e) }}
	case 5:
		return symCase{mul(x.s, y.s), func(e map[string]uint32) uint32 { return x.eval(e) * y.eval(e) }}
	case 6:
		n := uint32(r.IntN(32))
		return symCase{shl(x.s, n), func(e map[string]uint32) uint32 { return x.eval(e) << n }}
	case 7:
		n := uint32(r.IntN(32))
		return symCase{shr(x.s, n), func(e map[string]uint32) uint32 { return x.eval(e) >> n }}
	case 8:
		return symCase{neg(x.s), func(e map[string]uint32) uint32 { return -x.eval(e) }}
	case 9:
		return symCase{not(x.s), func(e map[string]uint32) uint32 { return ^x.eval(e) }}
	case 10:
		m := k()
		return symCase{andc(x.s, m), func(e map[string]uint32) uint32 { return x.eval(e) & m }}
	case 11:
		// ((x - ((2*x) & m)) + d) ^ d, sometimes with m = 2d
		m, d := k(), k()
		if r.IntN(2) == 0 {
			m = 2 * d
		}
		s := xor(add(sub(x.s, and(mul(cnst(2), x.s), cnst(m))), cnst(d)), cnst(d))
		return symCase{s, func(e map[string]uint32) uint32 { v := x.eval(e); return ((v - ((2 * v) & m)) + d) ^ d }}
	case 12:
		// x + ((2*x & m) ^ c), the carry-free form of x - 2(x&k) - c'
		m, c := k(), k()
		s := add(x.s, xor(and(shl(x.s, 1), cnst(m)), cnst(c)))
		return symCase{s, func(e map[string]uint32) uint32 { v := x.eval(e); return v + ((v << 1 & m) ^ c) }}
	case 14:
		// (x + y) - ((2x + 2y + 2j) & m) + d: the masked operand is x + y + j
		j, m, d := k(), k(), k()
		s := add(sub(add(x.s, y.s), and(add(add(shl(x.s, 1), shl(y.s, 1)), cnst(2*j)), cnst(m))), cnst(d))
		return symCase{s, func(e map[string]uint32) uint32 {
			a, b := x.eval(e), y.eval(e)
			return (a + b) - ((2*a + 2*b + 2*j) & m) + d
		}}
	case 15:
		// a·(x & k) + b·(x ^ k) + c·x: linear MBA over one mask
		kk, ca, cb, cc := k(), k(), k(), k()
		s := add(add(mul(cnst(ca), andc(x.s, kk)), mul(cnst(cb), xor(x.s, cnst(kk)))), mul(cnst(cc), x.s))
		return symCase{s, func(e map[string]uint32) uint32 { v := x.eval(e); return ca*(v&kk) + cb*(v^kk) + cc*v }}
	default:
		// 0xFFFFFFFE*(x & k) + x + d
		kk, d := k(), k()
		s := add(add(mul(cnst(0xFFFFFFFE), and(x.s, cnst(kk))), x.s), cnst(d))
		return symCase{s, func(e map[string]uint32) uint32 { v := x.eval(e); return 0xFFFFFFFE*(v&kk) + v + d }}
	}
}

func TestSimplifyMatchesEvaluation(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	env := map[string]uint32{}
	for i := 0; i < 20000; i++ {
		c := genSym(r, 1+r.IntN(5))
		for j := 0; j < 16; j++ {
			for _, l := range symLeaves {
				env[l.name] = r.Uint32() & l.po
			}
			got, want := c.s.eval(env), c.eval(env)
			if got != want {
				code, _ := c.s.render()
				t.Fatalf("case %d: simplified %s = %#x, want %#x (env %v)", i, code, got, want, env)
			}
			if got&^c.s.po != 0 {
				code, _ := c.s.render()
				t.Fatalf("case %d: %s = %#x has bits outside its possible ones %#x", i, code, got, c.s.po)
			}
		}
	}
}

// The patterns upstream is built from fold away completely.
func TestSimplifyFoldsMBA(t *testing.T) {
	x := leaf("x", precPrimary, all, true)
	b := leaf("b", precPrimary, 0xFF, true)
	tests := []struct {
		name string
		got  *sym
		want string
	}{
		{"((x - (2x & 2k)) + k) ^ k", xor(add(sub(x, andc(shl(x, 1), 0x2468ACE0)), cnst(0x12345670)), cnst(0x12345670)), "x"},
		{"x - 2(x & k) + k", add(sub(x, scale(andc(x, 0x0F0F0F0F), 2)), cnst(0x0F0F0F0F)), "x ^ 0xF0F0F0F"},
		{"(((b^77) - ((2(b^77)) & m)) << 16) - c ^ c'", xor(sub(shl(sub(xor(b, cnst(77)), andc(shl(xor(b, cnst(77)), 1), 344)), 16), cnst(743702528)), cnst(3551264768)), "b << 16 ^ 0x4D0000"},
		{"byte mask is a no-op", andc(b, 0xFFFF), "b"},
		{"disjoint or is xor", or(shl(b, 8), b), "b << 8 ^ b"},
	}
	for _, tt := range tests {
		code, _ := tt.got.render()
		if code != tt.want {
			t.Errorf("%s: got %s, want %s", tt.name, code, tt.want)
		}
	}
}
