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

var wrapperTok = strings.Repeat("W", 200)

// cfg0 is the config every provider call passes: amdl takes no token from it.
func cfg0() config.Config { return config.Default() }

// reading is a provider whose wrapper holds val for every account.
func reading(val string) *Provider {
	return NewReading(func(string) (string, error) { return val, nil })
}

func TestToken(t *testing.T) {
	tests := []struct {
		name string
		p    *Provider
		want Token
	}{
		{"the wrapper's", reading(" " + wrapperTok + "\n"), Token{wrapperTok, SourceWrapper, implicitID}},
		{"none from the wrapper", reading(""), Token{Account: implicitID}},
		{"not a token", reading("short"), Token{Account: implicitID}},
		{"no source", New(), Token{Account: implicitID}},
		{"nil provider", nil, Token{Account: implicitID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Token(cfg0()); got != tt.want {
				t.Fatalf("Token = {%d bytes, %q}, want {%d bytes, %q}", len(got.Value), got.Source, len(tt.want.Value), tt.want.Source)
			}
		})
	}
}

func TestNilProviderWrapper(t *testing.T) {
	var p *Provider
	if v, err := p.Wrapper(cfg0(), implicitID); v != "" || err != nil {
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
				t.Fatalf("error %q carries the value", err)
			}
		})
	}
}

func TestInvalidWrapperTokenIsAnError(t *testing.T) {
	if _, err := reading("short").Wrapper(cfg0(), implicitID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Wrapper error = %v, want ErrInvalid", err)
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
