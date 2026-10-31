package usertoken

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

var (
	wrapperTok = strings.Repeat("W", 200)
	configTok  = strings.Repeat("C", 200)
)

func cfgWith(token, dataDir string) config.Config {
	cfg := config.Default()
	cfg.Catalog.MediaUserToken = token
	cfg.Wrapper.DataDir = dataDir
	return cfg
}

// managedReader returns a managed reader that answers val and counts calls.
func managedReader(val string, err error, calls *int) func() (string, error) {
	return func() (string, error) {
		*calls++
		return val, err
	}
}

func TestTokenPrecedence(t *testing.T) {
	var calls int
	tests := []struct {
		name string
		p    *Provider
		cfg  config.Config
		want Token
	}{
		{"wrapper beats config", New(managedReader(wrapperTok, nil, &calls)), cfgWith(configTok, ""), Token{wrapperTok, SourceWrapper}},
		{"config when the wrapper has none", New(managedReader("", nil, &calls)), cfgWith("  "+configTok+"\n", ""), Token{configTok, SourceConfig}},
		{"config when the wrapper file is unreadable", New(managedReader("", errors.New("boom"), &calls)), cfgWith(configTok, ""), Token{configTok, SourceConfig}},
		{"config when the wrapper file is not a token", New(managedReader("short", nil, &calls)), cfgWith(configTok, ""), Token{configTok, SourceConfig}},
		{"nil provider is config only", nil, cfgWith(configTok, ""), Token{configTok, SourceConfig}},
		{"no managed reader and no data_dir", New(nil), cfgWith(configTok, ""), Token{configTok, SourceConfig}},
		{"none anywhere", New(managedReader("", nil, &calls)), cfgWith("   ", ""), Token{}},
		{"nil provider and no config token", nil, cfgWith("", ""), Token{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Token(tt.cfg); got != tt.want {
				t.Fatalf("Token = {%d bytes, %q}, want {%d bytes, %q}", len(got.Value), got.Source, len(tt.want.Value), tt.want.Source)
			}
		})
	}
}

func TestNilProviderWrapper(t *testing.T) {
	var p *Provider
	if v, err := p.Wrapper(cfgWith(configTok, t.TempDir())); v != "" || err != nil {
		t.Fatalf("nil Wrapper = %d bytes, %v; want none", len(v), err)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{"trims surrounding whitespace", " \t" + wrapperTok + "\r\n", wrapperTok, true},
		{"shortest", strings.Repeat("a", 16), strings.Repeat("a", 16), true},
		{"longest", strings.Repeat("a", 4096), strings.Repeat("a", 4096), true},
		{"base64 and base64url", "Ab0+/=-_" + strings.Repeat("x", 12), "Ab0+/=-_" + strings.Repeat("x", 12), true},
		{"other punctuation", "Ab0~!" + strings.Repeat("x", 12), "", false},
		{"JSON", `{"music_token":"` + strings.Repeat("x", 20) + `"}`, "", false},
		{"too short", strings.Repeat("a", 15), "", false},
		{"too long", strings.Repeat("a", 4097), "", false},
		{"whitespace only", " \n ", "", false},
		{"inner space", strings.Repeat("a", 10) + " " + strings.Repeat("a", 10), "", false},
		{"control byte", strings.Repeat("a", 10) + "\x00" + strings.Repeat("a", 10), "", false},
		{"non-ASCII", strings.Repeat("a", 10) + "é" + strings.Repeat("a", 10), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Validate(tt.raw)
			if tt.ok {
				if err != nil || got != tt.want {
					t.Fatalf("Validate = %d bytes, %v; want %d bytes", len(got), err, len(tt.want))
				}
				return
			}
			if !errors.Is(err, ErrInvalid) || got != "" {
				t.Fatalf("Validate = %d bytes, %v; want ErrInvalid", len(got), err)
			}
			if strings.Contains(err.Error(), "aaaaaaaaaa") {
				t.Fatalf("error %q carries the file's contents", err)
			}
		})
	}
}

func TestInvalidWrapperTokenIsAnErrorAndNotCached(t *testing.T) {
	var calls int
	val := "short"
	p := New(func() (string, error) { calls++; return val, nil })
	if _, err := p.Wrapper(config.Default()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Wrapper error = %v, want ErrInvalid", err)
	}
	val = wrapperTok
	if v, err := p.Wrapper(config.Default()); err != nil || v != wrapperTok {
		t.Fatalf("after a valid write Wrapper = %d bytes, %v", len(v), err)
	}
	if calls != 2 {
		t.Fatalf("reads = %d, want 2 (an invalid read is not cached)", calls)
	}
}

func TestCacheThirtySeconds(t *testing.T) {
	var calls int
	val := wrapperTok
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	p := New(func() (string, error) { calls++; return val, nil })
	p.now = func() time.Time { return now }
	cfg := config.Default()

	if v, _ := p.Wrapper(cfg); v != wrapperTok || calls != 1 {
		t.Fatalf("first read: %d bytes, %d calls", len(v), calls)
	}
	val = strings.Repeat("N", 200)
	now = now.Add(29 * time.Second)
	if v, _ := p.Wrapper(cfg); v != wrapperTok || calls != 1 {
		t.Fatalf("within 30 s: got a fresh read (%d calls) or another value", calls)
	}
	now = now.Add(time.Second)
	if v, _ := p.Wrapper(cfg); v != val || calls != 2 {
		t.Fatalf("after 30 s: %d calls, want a fresh read of the new value", calls)
	}
}

func TestMissIsNotCached(t *testing.T) {
	var calls int
	val := ""
	p := New(func() (string, error) { calls++; return val, nil })
	now := time.Now()
	p.now = func() time.Time { return now }
	if v, err := p.Wrapper(config.Default()); v != "" || err != nil {
		t.Fatalf("no token: %d bytes, %v", len(v), err)
	}
	val = wrapperTok // a login lands
	if v, _ := p.Wrapper(config.Default()); v != wrapperTok {
		t.Fatal("a token written right after a miss is not seen on the next read")
	}
	if calls != 2 {
		t.Fatalf("reads = %d, want 2", calls)
	}
}

func writeFile(t *testing.T, path, body string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if !mod.IsZero() {
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDataDirLiteLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, FileName), wrapperTok+"\n", time.Time{})
	// A direct file wins over instances, whatever their age.
	writeFile(t, filepath.Join(dir, "instances", "a", FileName), configTok, time.Now().Add(time.Hour))
	got := New(nil).Token(cfgWith(configTok, dir))
	if got != (Token{wrapperTok, SourceWrapper}) {
		t.Fatalf("Token = %q source, %d bytes; want the direct MUSIC_TOKEN", got.Source, len(got.Value))
	}
}

func TestDataDirManagerLayoutNewestWins(t *testing.T) {
	dir := t.TempDir()
	old, newer := strings.Repeat("O", 100), strings.Repeat("N", 100)
	base := time.Now().Add(-time.Hour)
	writeFile(t, filepath.Join(dir, "instances", "11111111-aaaa", FileName), old, base)
	writeFile(t, filepath.Join(dir, "instances", "22222222-bbbb", FileName), newer, base.Add(10*time.Minute))
	// A stray file and an instance without a token are skipped.
	writeFile(t, filepath.Join(dir, "instances", "README"), "x", time.Time{})
	if err := os.MkdirAll(filepath.Join(dir, "instances", "33333333-cccc"), 0o755); err != nil {
		t.Fatal(err)
	}
	v, err := New(nil).Wrapper(cfgWith("", dir))
	if err != nil || v != newer {
		t.Fatalf("Wrapper = %d bytes, %v; want the newest instance's token", len(v), err)
	}
}

func TestDataDirMissingIsNoToken(t *testing.T) {
	for name, dir := range map[string]string{
		"missing directory":        filepath.Join(t.TempDir(), "nope"),
		"empty directory":          t.TempDir(),
		"instances without tokens": func() string { d := t.TempDir(); _ = os.MkdirAll(filepath.Join(d, "instances", "x"), 0o755); return d }(),
	} {
		t.Run(name, func(t *testing.T) {
			v, err := New(nil).Wrapper(cfgWith(configTok, dir))
			if v != "" || err != nil {
				t.Fatalf("Wrapper = %d bytes, %v; want no token", len(v), err)
			}
			if got := New(nil).Token(cfgWith(configTok, dir)); got.Source != SourceConfig {
				t.Fatalf("Token source = %q, want config", got.Source)
			}
		})
	}
}

func TestDataDirInvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, FileName), strings.Repeat("a", 3*maxLen), time.Time{})
	if _, err := New(nil).Wrapper(cfgWith("", dir)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized file: err = %v, want ErrInvalid", err)
	}
}

func TestDataDirIgnoredWhenManaged(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, FileName), wrapperTok, time.Time{})
	var calls int
	p := New(managedReader("", nil, &calls))
	if v, err := p.Wrapper(cfgWith("", dir)); v != "" || err != nil {
		t.Fatalf("managed with no token read data_dir: %d bytes, %v", len(v), err)
	}
	if calls != 1 {
		t.Fatalf("managed reader calls = %d, want 1", calls)
	}
	if got := p.Token(cfgWith(configTok, dir)); got.Source != SourceConfig {
		t.Fatalf("Token source = %q, want config", got.Source)
	}
}

func TestCacheIsPerSource(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	tokA, tokB := strings.Repeat("A", 200), strings.Repeat("B", 200)
	writeFile(t, filepath.Join(a, FileName), tokA, time.Time{})
	writeFile(t, filepath.Join(b, FileName), tokB, time.Time{})
	p := New(nil)
	if v, _ := p.Wrapper(cfgWith("", a)); v != tokA {
		t.Fatal("first data_dir")
	}
	if v, _ := p.Wrapper(cfgWith("", b)); v != tokB {
		t.Fatal("a second data_dir got the first one's cached token")
	}
}
