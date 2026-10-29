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
			[]string{"x_2:=0-(a<<1)", "x_3:=x_2^1", "returnx_3"},
		},
		{
			"a shadowed parameter is reassigned",
			`pub fn f(v: u32) -> u32 { let mut v = v ^ 1u32; v }`,
			[]string{"v_2:=v^1", "returnv_2"},
		},
		{
			"let mut of a parameter emits nothing",
			`pub fn f(a: u32) -> u32 { let mut a = a; a ^= 1u32; a }`,
			[]string{"{a_2:=a^1returna_2}"},
		},
		{
			"an unread local is dropped",
			`pub fn f(a: u32) -> u32 { let mut y = a; a }`,
			[]string{"{returna}"},
		},
		{
			"an unread slice read stays and is blanked",
			`pub fn f(ct: &[u8], a: u32) -> u32 { let y = (ct[(a & 0xFFu32) as usize] as u32); a }`,
			[]string{"y:=uint32(ct[uint(a&255)])_=y"},
		},
		{
			"a reassigned local gets a new name, so earlier reads keep their value",
			`pub fn f(st: &mut [u32; 4]) -> u32 { let mut x = st[(0u32) as usize]; st[(0u32) as usize] = x ^ (1u32); let y = x; x = st[(0u32) as usize]; x ^ y }`,
			[]string{"x:=st[0]", "st[0]=x^1", "x_2:=st[0]", "returnx^x_2"},
		},
		{
			"a local assigned in a branch stays one variable",
			`pub fn f(a: u32) -> u32 { let mut r = a ^ 1u32; let y = r; if (a == 0u32) { r = a; } r ^ y }`,
			[]string{"r:=a^1y:=r", "ifa==0{r=a}", "returnr^y"},
		},
		{
			"Rust precedence survives Go's",
			`pub fn f(a: u32, b: u32, c: u32) -> u32 { let x = a ^ b & c; let y = a + b << 2u32; let z = a | b ^ c; x ^ y ^ z }`,
			[]string{"y:=a<<2+b<<2", "z:=a|(b^c)", "returnb&c^y^a^z"},
		},
		{
			"mixed boolean-arithmetic folds",
			`pub fn f(a: u32) -> u32 { let mut x = a; let mut t = ((2u32)).wrapping_mul(x); t &= (0x2468ACE0u32); x = (x).wrapping_sub(t); x = (x).wrapping_add((0x12345670u32)); x ^= (0x12345670u32); x }`,
			[]string{"{returna}"},
		},
		{
			"a loop over a literal array is unrolled",
			`pub fn f(st: &mut [u32; 4]) -> u32 { for &(d, s) in [((1u32),(2u32)), ((3u32),(0u32))].iter() { st[d as usize] = st[s as usize] ^ (1u32); } st[(0u32) as usize] }`,
			[]string{"{d:=uint32(1)s:=uint32(2)st[uint(d)]=st[uint(s)]^1}{d_2:=uint32(3)s_2:=uint32(0)st[uint(d_2)]=st[uint(s_2)]^1}"},
		},
		{
			"not is bitwise on integers and logical on bools",
			`pub fn f(a: u32) -> u32 { let mut r = !a; let c = (r == a); if (!c) { r = !0xFFu32; } r }`,
			[]string{"r:=a^0xFFFFFFFF", "c:=r==a", "if!c{r=0xFFFFFF00}"},
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
