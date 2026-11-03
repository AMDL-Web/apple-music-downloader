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

// dataDirWith is a wrapper.data_dir whose MUSIC_TOKEN holds val, or none
// when val is empty.
func dataDirWith(t *testing.T, val string) string {
	t.Helper()
	dir := t.TempDir()
	if val != "" {
		writeFile(t, filepath.Join(dir, FileName), val, time.Time{})
	}
	return dir
}

func TestTokenPrecedence(t *testing.T) {
	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, FileName), 0o700); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		p    *Provider
		cfg  config.Config
		want Token
	}{
		{"wrapper beats config", New(), cfgWith(configTok, dataDirWith(t, wrapperTok)), Token{wrapperTok, SourceWrapper, implicitID}},
		{"config when the wrapper has none", New(), cfgWith("  "+configTok+"\n", dataDirWith(t, "")), Token{configTok, SourceConfig, implicitID}},
		{"config when the wrapper file is unreadable", New(), cfgWith(configTok, unreadable), Token{configTok, SourceConfig, implicitID}},
		{"config when the wrapper file is not a token", New(), cfgWith(configTok, dataDirWith(t, "short")), Token{configTok, SourceConfig, implicitID}},
		{"nil provider is config only", nil, cfgWith(configTok, ""), Token{configTok, SourceConfig, implicitID}},
		{"no data_dir", New(), cfgWith(configTok, ""), Token{configTok, SourceConfig, implicitID}},
		{"none anywhere", New(), cfgWith("   ", dataDirWith(t, "")), Token{Account: implicitID}},
		{"nil provider and no config token", nil, cfgWith("", ""), Token{Account: implicitID}},
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
	if v, err := p.Wrapper(cfgWith(configTok, t.TempDir()), implicitID); v != "" || err != nil {
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

func TestInvalidWrapperTokenIsAnError(t *testing.T) {
	dir := dataDirWith(t, "short")
	p := New()
	if _, err := p.Wrapper(cfgWith("", dir), implicitID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Wrapper error = %v, want ErrInvalid", err)
	}
	writeFile(t, filepath.Join(dir, FileName), wrapperTok, time.Time{})
	if v, err := p.Wrapper(cfgWith("", dir), implicitID); err != nil || v != wrapperTok {
		t.Fatalf("after a valid write Wrapper = %d bytes, %v", len(v), err)
	}
}

// Every use reads the file afresh: a refreshed token is used at once, and a
// token written right after a miss is seen on the next read.
func TestReadsEveryTime(t *testing.T) {
	dir := dataDirWith(t, "")
	p := New()
	cfg := cfgWith("", dir)
	if v, err := p.Wrapper(cfg, implicitID); v != "" || err != nil {
		t.Fatalf("no token: %d bytes, %v", len(v), err)
	}
	writeFile(t, filepath.Join(dir, FileName), wrapperTok, time.Time{}) // a login lands
	if v, _ := p.Wrapper(cfg, implicitID); v != wrapperTok {
		t.Fatal("a token written right after a miss is not seen on the next read")
	}
	refreshed := strings.Repeat("N", 200)
	writeFile(t, filepath.Join(dir, FileName), refreshed, time.Time{})
	if v, _ := p.Wrapper(cfg, implicitID); v != refreshed {
		t.Fatal("a refreshed token is not read afresh")
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
	got := New().Token(cfgWith(configTok, dir))
	if got != (Token{wrapperTok, SourceWrapper, implicitID}) {
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
	v, err := New().Wrapper(cfgWith("", dir), implicitID)
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
			v, err := New().Wrapper(cfgWith(configTok, dir), implicitID)
			if v != "" || err != nil {
				t.Fatalf("Wrapper = %d bytes, %v; want no token", len(v), err)
			}
			if got := New().Token(cfgWith(configTok, dir)); got.Source != SourceConfig {
				t.Fatalf("Token source = %q, want config", got.Source)
			}
		})
	}
}

func TestDataDirInvalidFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, FileName), strings.Repeat("a", 3*maxLen), time.Time{})
	if _, err := New().Wrapper(cfgWith("", dir), implicitID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized file: err = %v, want ErrInvalid", err)
	}
}

func TestEachDataDirReadsItsOwn(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	tokA, tokB := strings.Repeat("A", 200), strings.Repeat("B", 200)
	writeFile(t, filepath.Join(a, FileName), tokA, time.Time{})
	writeFile(t, filepath.Join(b, FileName), tokB, time.Time{})
	p := New()
	if v, _ := p.Wrapper(cfgWith("", a), implicitID); v != tokA {
		t.Fatal("first data_dir")
	}
	if v, _ := p.Wrapper(cfgWith("", b), implicitID); v != tokB {
		t.Fatal("a second data_dir got the first one's token")
	}
}
