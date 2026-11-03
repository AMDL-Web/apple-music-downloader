package wrapperhost

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// GuestRepo builds the manager guest and the wrapper-lite payload on its amdl
// branch, and its publish workflow puts each successful build on amdl up as a
// release asset: actions artifacts download at about 30 KB/s from China and
// expire, release assets come from GitHub's CDN and stay.
const GuestRepo = "AMDL-Web/wrapper"

// GuestArtifact is the manager.yml artifact, including the kernel and disk,
// published as <GuestArtifact>.zip in the release guest-<commit12>.
const GuestArtifact = "manager-qemu-guest"

// LiteArtifact is the build-lite artifact holding the wrapper-lite payload,
// published as <LiteArtifact>.zip in the release lite-<commit12>. The guest
// is x86_64 whatever the host, so this is the only one amdl pins.
const LiteArtifact = "wrapper-lite-linux-x86_64"

// Asset is one file amdl downloads: where from, and the exact size and
// sha256 it must have.
type Asset struct {
	URL    string
	SHA256 string
	Size   int64
}

// releaseURL is where GuestRepo's publish workflow puts artifact for commit.
func releaseURL(prefix, commit, artifact string) string {
	return fmt.Sprintf("https://github.com/%s/releases/download/%s-%s/%s.zip", GuestRepo, prefix, commit[:min(12, len(commit))], artifact)
}

// LitePin names one build-lite build on amdl and the exact zip digest of the
// wrapper-lite payload the guest's manager installs into its data disk on
// first boot. amdl downloads it on the host and serves it to the guest; the
// manager is told the digest, so it installs nothing else.
type LitePin struct {
	Artifact string `json:"artifact"`
	Commit   string `json:"commit"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

// Asset is the payload's release asset.
func (l LitePin) Asset() Asset {
	return Asset{URL: releaseURL("lite", l.Commit, l.Artifact), SHA256: l.SHA256, Size: l.Size}
}

// complete reports whether l names a payload.
func (l LitePin) complete() error {
	if l.Artifact != LiteArtifact || len(l.Commit) < 12 || !hexSHA.MatchString(l.SHA256) || l.Size <= 0 {
		return fmt.Errorf("the wrapper-lite pin for commit %q is incomplete", l.Commit)
	}
	return nil
}

// guestHost is the host as the guest's user-mode network reaches it: QEMU
// forwards 10.0.2.2 to the host's loopback.
const guestHost = "10.0.2.2"

// managerArgs are the manager's arguments for one boot, one per line in the
// fw_cfg file the guest's init splits on whitespace: the listen address its
// default would give, and the pinned payload at payloadURL, where the host
// serves it. Without -payload-url the manager downloads the wrapper-lite
// release it pins itself.
func managerArgs(l LitePin, payloadURL string) []string {
	return []string{"-host", "0.0.0.0", "-port", fmt.Sprint(guestPort), "-payload-url", payloadURL, "-payload-sha256", l.SHA256}
}

// Pin names one successful manager build on amdl, the exact zip digest of its
// release asset, and the wrapper-lite payload that guest installs.
type Pin struct {
	Artifact string  `json:"artifact"`
	Commit   string  `json:"commit"`
	SHA256   string  `json:"sha256"`
	Size     int64   `json:"size"`
	Lite     LitePin `json:"lite"`
}

// Asset is the guest's release asset.
func (p Pin) Asset() Asset {
	return Asset{URL: releaseURL("guest", p.Commit, p.Artifact), SHA256: p.SHA256, Size: p.Size}
}

// Builtin pins a successful manager build on amdl and a successful build-lite
// build; SHA256 is GitHub's digest of each release asset. Install verifies
// both downloads before unpacking, and the manager verifies the payload.
var Builtin = Pin{
	Artifact: GuestArtifact,
	Commit:   "90a0be8ace2fbd0c30882740e77050f02832ad0f",
	SHA256:   "d28b3141f7a05d67fd9417a949bfa0ef8cf2f8fbb114ec1a8515d41b19fd835b",
	Size:     21260294,
	Lite: LitePin{
		Artifact: LiteArtifact,
		Commit:   "8885e67ac96b2b0c370f18e5b7c5a7edc5f19190",
		SHA256:   "10e16cdce9ee2648e1bfcbd3b8b138737bb90041e7a27e7971830c6a6aefe290",
		Size:     30641054,
	},
}

// ManifestURL serves the current pin as JSON, kept on its own branch so that
// every amdl release reads the same file.
const ManifestURL = "https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/wrapper-pins/wrapper-manager-guest.json"

var hexSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (p Pin) valid() error {
	if p.Artifact != GuestArtifact || len(p.Commit) < 12 || !hexSHA.MatchString(p.SHA256) || p.Size <= 0 {
		return fmt.Errorf("the guest pin for commit %q is incomplete", p.Commit)
	}
	return p.Lite.complete()
}

// manifestTimeout bounds the manifest read; the builtin pin is the fallback.
const manifestTimeout = 10 * time.Second

// CurrentPin returns the published pin when ManifestURL answers with a valid
// one, else Builtin, saying which.
func CurrentPin(ctx context.Context, c *http.Client) (Pin, string, error) {
	if p, err := fetchPin(ctx, c); err == nil && p.valid() == nil {
		return p, "manifest", nil
	}
	if err := Builtin.valid(); err != nil {
		return Pin{}, "", fmt.Errorf("no current wrapper download: the built-in %w and %s did not give a valid one; "+
			"download the manager-qemu-guest release asset yourself and run amdl wrapper import <zip>", err, ManifestURL)
	}
	return Builtin, "builtin", nil
}

func fetchPin(ctx context.Context, c *http.Client) (Pin, error) {
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ManifestURL, nil)
	if err != nil {
		return Pin{}, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return Pin{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Pin{}, fmt.Errorf("%s: %s", ManifestURL, resp.Status)
	}
	var p Pin
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&p)
	return p, err
}
