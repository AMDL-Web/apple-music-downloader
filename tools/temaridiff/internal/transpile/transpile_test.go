package transpile

import (
	"strings"
	"testing"
)

func squash(s string) string {
	return strings.NewReplacer(" ", "", "\t", "", "\n", "").Replace(s)
}

// Expected Go comes from Rust's semantics: release-build u32 wrapping, Rust
// operator precedence, wrapping_shl masking its amount, shadowing.
func TestGenerate(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{
		{
			"constant subexpressions fold with u32 wrapping",
			`pub fn f(a: u32) -> u32 { let mut x = ((2u32)).wrapping_neg(); x = (x).wrapping_mul(a); x = x ^ (4294967295u32).wrapping_add(2u32); x }`,
			[]string{"x:=uint32(0xFFFFFFFE)", "x=x*a", "x=x^1", "returnx"},
		},
		{
			"a shadowed parameter is reassigned",
			`pub fn f(v: u32) -> u32 { let mut v = v ^ 1u32; v }`,
			[]string{"v=v^1", "returnv"},
		},
		{
			"let mut of a parameter emits nothing",
			`pub fn f(a: u32) -> u32 { let mut a = a; a ^= 1u32; a }`,
			[]string{"{a^=1returna}"},
		},
		{
			"an unread local is blanked",
			`pub fn f(a: u32) -> u32 { let mut y = a; a }`,
			[]string{"y:=a_=y"},
		},
		{
			"Rust precedence survives Go's",
			`pub fn f(a: u32, b: u32, c: u32) -> u32 { let x = a ^ b & c; let y = a + b << 2u32; let z = a | b ^ c; x ^ y ^ z }`,
			[]string{"x:=a^(b&c)", "y:=(a+b)<<2", "z:=a|(b^c)", "returnx^y^z"},
		},
		{
			"not is bitwise on integers and logical on bools",
			`pub fn f(a: u32) -> u32 { let mut r = !a; let c = (r == a); if (!c) { r = !0xFFu32; } r }`,
			[]string{"r:=^a", "c:=r==a", "if!c{r=0xFFFFFF00}"},
		},
		{
			"wrapping_shl masks its amount",
			`pub fn f(a: u32) -> u32 { (a).wrapping_shl(33) }`,
			[]string{"returna<<1"},
		},
		{
			"casts and byte slices",
			`pub fn f(ct: &[u8], i: u32) -> u32 { (ct[(i & 0xFFFu32) as usize] as u32) }`,
			[]string{"returnuint32(ct[uint(i&0xFFF)])"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := generate([]byte(tt.src), "test", nil)
			if err != nil {
				t.Fatal(err)
			}
			got := squash(string(out))
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
		})
	}
}

func TestGenerateRejects(t *testing.T) {
	tests := []struct {
		name   string
		src    string
		pinned map[string]string
	}{
		{"shift past the width", `pub fn f(a: u32) -> u32 { a << 32u32 }`, nil},
		{"unknown method", `pub fn f(a: u32) -> u32 { a.rotate_left(3) }`, nil},
		{"block comment", `/* x */ pub fn f() {}`, nil},
		{"self-assignment", `pub fn f(a: u32) -> u32 { let mut x = a; x = x; x }`, nil},
		{"changed hand-ported helper", `pub fn g(a: u32) -> u32 { a }`, map[string]string{"g": "pinned"}},
		{"removed hand-ported helper", `pub fn f(a: u32) -> u32 { a }`, map[string]string{"g": "pinned"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := generate([]byte(tt.src), "test", tt.pinned); err == nil {
				t.Fatal("generate succeeded")
			}
		})
	}
}
