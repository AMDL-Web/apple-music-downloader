package wrapperhost

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArgs(t *testing.T) {
	o := RunOpts{Accel: "whpx", Kernel: "/k", Initrd: "/i", Image: "/a,b/data.img", HostPort: 52011, GuestPort: 8080,
		MemoryMB: 512, CPUs: 2, ArgsFile: "/s/login-args"}
	// upstream buildQemuArgs at bde3335 for WHPX, minus -L and lite_args_b64,
	// with hostfwd on loopback; a comma in a path is doubled for QEMU.
	want := "-accel whpx,kernel-irqchip=off -cpu qemu64-v1 -m 512 -smp 2 -kernel /k -initrd /i " +
		"-append console=ttyS0 quiet net.ifnames=0 biosdevname=0 -display none -serial stdio -no-reboot " +
		"-nic user,model=e1000,hostfwd=tcp:127.0.0.1:52011-:8080 -drive file=/a,,b/data.img,format=raw,if=virtio " +
		"-fw_cfg name=lite_args,file=/s/login-args"
	if got := strings.Join(Args(o), " "); got != want {
		t.Errorf("Args =\n%s\nwant\n%s", got, want)
	}
	o.Accel, o.ArgsFile = "kvm", ""
	got := strings.Join(Args(o), " ")
	if !strings.HasPrefix(got, "-accel kvm -cpu host ") || strings.Contains(got, "fw_cfg") {
		t.Errorf("serve boot on KVM: %s", got)
	}
	o.Accel = "tcg"
	if !strings.HasPrefix(strings.Join(Args(o), " "), "-accel tcg -cpu max ") {
		t.Error("TCG runs -cpu max")
	}
}

func TestAccel(t *testing.T) {
	for _, c := range []struct {
		goos, goarch string
		kvm          bool
		want         string
	}{
		{"linux", "amd64", true, "kvm"}, {"linux", "amd64", false, "tcg"},
		{"windows", "amd64", false, "whpx"},
		{"darwin", "amd64", false, "hvf"}, {"darwin", "arm64", false, "tcg"},
	} {
		if got := Accel(c.goos, c.goarch, c.kvm); got != c.want {
			t.Errorf("Accel(%s/%s, kvm %v) = %s, want %s", c.goos, c.goarch, c.kvm, got, c.want)
		}
	}
}

func TestFindQEMU(t *testing.T) {
	none := func(string) (string, error) { return "", errors.New("not found") }
	at := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, q := range paths {
				if p == q {
					return true
				}
			}
			return false
		}
	}
	if p, err := FindQEMU("", "darwin", none, at("/opt/homebrew/bin/qemu-system-x86_64")); err != nil || p != "/opt/homebrew/bin/qemu-system-x86_64" {
		t.Errorf("Homebrew's QEMU: %q, %v", p, err)
	}
	if p, _ := FindQEMU("", "darwin", func(string) (string, error) { return "/x/qemu-system-x86_64", nil }, at()); p != "/x/qemu-system-x86_64" {
		t.Errorf("PATH comes first: %q", p)
	}
	if _, err := FindQEMU("", "windows", none, at()); !errors.Is(err, ErrNoQEMU) {
		t.Errorf("no QEMU: %v", err)
	}
	if _, err := FindQEMU("/nope", "linux", none, at()); err == nil {
		t.Error("a configured path that does not exist fails")
	}
	if cmd, run := QEMUInstall("darwin", func(m string) bool { return m == "brew" }); cmd != "brew install qemu" || !run {
		t.Errorf("Mac with Homebrew: %q %v", cmd, run)
	}
	if cmd, run := QEMUInstall("linux", func(m string) bool { return m == "apt-get" }); cmd != "sudo apt-get install qemu-system-x86" || run {
		t.Errorf("Debian: %q %v", cmd, run)
	}
}

// fakeArtifact is a zip shaped like upstream's qemu-assets artifact, or with
// prefix like a wrapper-lite-qemu package.
func fakeArtifact(t *testing.T, prefix, image string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, body := range map[string]string{kernelName: "kernel", initrdName: "initrd", imageName: image, "LICENSE": "MIT"} {
		w, err := z.Create(prefix + name)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, body)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fakeFetcher struct {
	body  []byte
	calls []int64
}

func (f *fakeFetcher) Fetch(_ context.Context, _ Pin, offset int64, w io.Writer) error {
	f.calls = append(f.calls, offset)
	_, err := w.Write(f.body[offset:])
	return err
}

func pinFor(body []byte, commit string) Pin {
	sum := sha256.Sum256(body)
	return Pin{Commit: commit, ArtifactID: 7, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}
}

func TestDownloadResumesAndVerifies(t *testing.T) {
	body := bytes.Repeat([]byte("abc"), 1000)
	path := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(path, body[:1200], 0o600)
	f := &fakeFetcher{body: body}
	if err := Download(context.Background(), f, pinFor(body, "c"), path, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0] != 1200 {
		t.Errorf("fetch offsets %v, want a resume at 1200", f.calls)
	}
	bad := pinFor(body, "c")
	bad.SHA256 = strings.Repeat("0", 64)
	if err := Download(context.Background(), f, bad, path, nil); !errors.Is(err, ErrChecksum) {
		t.Errorf("mismatch: %v", err)
	}
	if st, _ := os.Stat(path); st.Size() != 0 {
		t.Error("a bad download is discarded")
	}
}

func TestInstallReportsEachStep(t *testing.T) {
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	art := fakeArtifact(t, "", "empty image")
	p := pinFor(art, "bde3335f47a30518")
	// A partial file left by an earlier attempt: the count starts from it.
	h.prepare()
	os.WriteFile(h.path("download-"+p.SHA256[:12]+".zip"), art[:100], 0o600)
	var got []Progress
	if _, err := h.Install(context.Background(), &fakeFetcher{body: art}, p, time.Now(), func(pr Progress) { got = append(got, pr) }); err != nil {
		t.Fatal(err)
	}
	want := []Progress{
		{Downloading, 100, p.Size},
		{Downloading, p.Size, p.Size},
		{Verifying, p.Size, p.Size},
		{Phase: Unpacking},
	}
	if len(got) != len(want) {
		t.Fatalf("reports = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("report %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestInstallKeepsTheAccount(t *testing.T) {
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	first := fakeArtifact(t, "", "empty image")
	in, err := h.Install(context.Background(), &fakeFetcher{body: first}, pinFor(first, "bde3335f47a30518"), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if in.ID != "bde3335f47a3" || in.Port == 0 {
		t.Errorf("installed %+v", in)
	}
	st, err := os.Stat(h.ImagePath())
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("data.img: %v %v", st, err)
	}
	if st, _ := os.Stat(h.Dir); st.Mode().Perm() != 0o700 {
		t.Errorf("wrapper dir mode %v", st.Mode().Perm())
	}
	os.WriteFile(h.ImagePath(), []byte("logged-in tokens"), 0o600)
	second := fakeArtifact(t, "qemu/", "another empty image")
	up, err := h.Install(context.Background(), &fakeFetcher{body: second}, pinFor(second, "0123456789abcdef"), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(h.ImagePath()); string(got) != "logged-in tokens" || up.Port != in.Port {
		t.Errorf("an update replaced the account (%q) or moved the port (%d → %d)", got, in.Port, up.Port)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "assets", in.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old guest files stay")
	}
	k, _ := h.assets(up)
	if got, _ := os.ReadFile(k); string(got) != "kernel" {
		t.Errorf("kernel = %q", got)
	}
	if err := h.Uninstall(false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Current(); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("after uninstall: %v", err)
	}
	if _, err := os.Stat(h.ImagePath()); err != nil {
		t.Error("uninstall keeps the account unless purged")
	}
}

func TestImportRejectsAnUnrelatedZip(t *testing.T) {
	h := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("README.md")
	io.WriteString(w, "hi")
	z.Close()
	p := filepath.Join(t.TempDir(), "x.zip")
	os.WriteFile(p, b.Bytes(), 0o600)
	if _, err := h.Import(p, time.Now()); err == nil || !strings.Contains(err.Error(), "has no") {
		t.Errorf("import of an unrelated zip: %v", err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCurrentPin(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	down := &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
	if p, src, err := CurrentPin(context.Background(), down, now); err != nil || src != "builtin" || p != Builtin {
		t.Errorf("offline: %v %s %v", p, src, err)
	}
	if _, _, err := CurrentPin(context.Background(), down, Builtin.Expires.Add(time.Hour)); err == nil ||
		!strings.Contains(err.Error(), "amdl wrapper import") {
		t.Errorf("expired builtin and no manifest: %v", err)
	}
	newer := `{"commit":"c0ffee","run_id":2,"artifact_id":99,"sha256":"` + strings.Repeat("a", 64) + `","size":5,"expires_at":"2027-03-01T00:00:00Z"}`
	up := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(newer)), Request: r}, nil
	})}
	if p, src, _ := CurrentPin(context.Background(), up, now); src != "manifest" || p.ArtifactID != 99 {
		t.Errorf("manifest: %v %s", p, src)
	}
}
