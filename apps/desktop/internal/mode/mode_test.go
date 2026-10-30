package mode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/client"
	"amdl/internal/config"
)

func testDirs(t *testing.T) Dirs {
	root := t.TempDir()
	return Dirs{Data: filepath.Join(root, "data"), Cache: filepath.Join(root, "cache"), Music: filepath.Join(root, "music")}
}

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadSettings(dir)
	if err != nil || s != DefaultSettings() {
		t.Fatalf("first launch: %+v, %v", s, err)
	}
	s.Mode, s.RemoteURL = Remote, "http://192.168.3.5:18570"
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(dir)
	if err != nil || got != s {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestWriteConfigKeepsBackendEditsAndOwnsItsKeys(t *testing.T) {
	d := testDirs(t)
	b := Backend{Dirs: d, Wrapper: "http://192.168.3.5:12340", DownloadsDir: d.DefaultDownloads(), FFmpeg: "/opt/ffmpeg"}
	if err := b.WriteConfig(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(b.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	for key, p := range map[string]string{"database.path": cfg.Database.Path, "download.temp_dir": cfg.Download.TempDir, "download.downloads_dir": cfg.Download.DownloadsDir} {
		if !filepath.IsAbs(p) {
			t.Errorf("%s = %q, want an absolute path", key, p)
		}
	}
	// A setting changed through the backend's API stays; the app's own keys
	// follow its settings on the next start.
	raw := []byte(strings.Replace(mustRead(t, b.ConfigPath()), "- alac", "- aac", 1))
	if err := os.WriteFile(b.ConfigPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	b.Wrapper = "http://10.0.0.2:12340"
	if err := b.WriteConfig(); err != nil {
		t.Fatal(err)
	}
	again, err := config.Load(b.ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(again.Download.QualityPriority) != "[aac]" || again.Wrapper.Address != "http://10.0.0.2:12340" || again.Tools.FFmpeg != "/opt/ffmpeg" {
		t.Errorf("second start: quality %v, wrapper %q, ffmpeg %q", again.Download.QualityPriority, again.Wrapper.Address, again.Tools.FFmpeg)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestWriteConfigRejectsBadWrapper(t *testing.T) {
	d := testDirs(t)
	if err := (Backend{Dirs: d, Wrapper: "ftp://192.168.3.5:12340", DownloadsDir: d.DefaultDownloads()}).WriteConfig(); err == nil {
		t.Fatal("a wrapper address that is not http or https was accepted")
	}
}

func TestStandaloneStartsAndStops(t *testing.T) {
	d := testDirs(t)
	// No wrapper needs to answer for the backend to start.
	r, err := Backend{Dirs: d, Wrapper: "http://127.0.0.1:1", DownloadsDir: d.DefaultDownloads()}.Start()
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.New(r.Base)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.System(context.Background()); err != nil {
		t.Errorf("system: %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.ServerDir(), "db", "amdl.db")); err != nil {
		t.Errorf("database not under the app's data directory: %v", err)
	}
}

func TestWrapperTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"regions":["cn","us"]}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	checks := TestWrapper(context.Background(), srv.URL)
	if len(checks) != 2 || !checks[0].OK || !checks[1].OK || !strings.Contains(checks[1].Text, "cn, us") || checks[1].Note != "wrapper-lite" {
		t.Fatalf("checks %v", checks)
	}
	if bad := TestWrapper(context.Background(), "http://127.0.0.1:1"); len(bad) != 1 || bad[0].OK {
		t.Errorf("unreachable wrapper: %v", bad)
	}
}

func TestWrapperURLAddsTheScheme(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:12340":         "http://127.0.0.1:12340",
		" http://nas:12340 ":      "http://nas:12340",
		"https://wrapper.example": "https://wrapper.example",
		"":                        "",
	} {
		if got := WrapperURL(in); got != want {
			t.Errorf("WrapperURL(%q) = %q, want %q", in, got, want)
		}
	}
}
