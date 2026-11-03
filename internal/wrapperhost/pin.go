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

// GuestRepo builds the manager guest on its lite branch.
const GuestRepo = "AMDL-Web/wrapper"

// GuestArtifact is the manager.yml artifact, including the kernel and disk.
const GuestArtifact = "manager-qemu-guest"

// LiteArtifact is the build-lite artifact holding the wrapper-lite payload.
// The guest is x86_64 whatever the host, so this is the only one amdl pins.
const LiteArtifact = "wrapper-lite-linux-x86_64"

// LitePin names one successful build-lite run on lite and the exact zip
// digest of the wrapper-lite payload the guest's manager installs into its
// data disk on first boot. The manager is told this URL and digest, so it
// installs nothing else.
type LitePin struct {
	Artifact string    `json:"artifact"`
	Commit   string    `json:"commit"`
	RunID    int64     `json:"run_id"`
	SHA256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	Expires  time.Time `json:"expires_at"`
}

// URL is where the manager downloads the payload: the run's own artifact, not
// the newest one on lite.
func (l LitePin) URL() string {
	return fmt.Sprintf("https://nightly.link/%s/actions/runs/%d/%s.zip", GuestRepo, l.RunID, l.Artifact)
}

// complete reports whether l names a payload; expiry only matters to a
// download, and a payload already on the disk needs none.
func (l LitePin) complete() error {
	if l.Artifact != LiteArtifact || l.RunID <= 0 || l.Commit == "" || !hexSHA.MatchString(l.SHA256) || l.Size <= 0 {
		return fmt.Errorf("the wrapper-lite pin for run %d is incomplete", l.RunID)
	}
	return nil
}

func (l LitePin) valid(now time.Time) error {
	if err := l.complete(); err != nil {
		return err
	}
	if !l.Expires.IsZero() && now.After(l.Expires) {
		return fmt.Errorf("wrapper-lite run %d expired on %s", l.RunID, l.Expires.Format("2006-01-02"))
	}
	return nil
}

// managerArgs are the manager's arguments for one boot, one per line in the
// fw_cfg file the guest's init splits on whitespace: the listen address its
// default would give, and the pinned payload. Without -payload-url the
// manager installs the newest build-lite artifact, whatever its digest.
func managerArgs(l LitePin) []string {
	return []string{"-host", "0.0.0.0", "-port", fmt.Sprint(guestPort), "-payload-url", l.URL(), "-payload-sha256", l.SHA256}
}

// Pin names one successful manager workflow run on lite and the exact zip
// digest, with the wrapper-lite payload that guest installs. Artifacts
// expire; ManifestURL serves a refreshed pin.
type Pin struct {
	Artifact   string    `json:"artifact"`
	Commit     string    `json:"commit"`
	RunID      int64     `json:"run_id"`
	ArtifactID int64     `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	Expires    time.Time `json:"expires_at"`
	Lite       LitePin   `json:"lite"`
}

// Builtin pins a successful manager.yml run on lite and a successful
// build-lite run; SHA256 is GitHub's artifact digest; install verifies the
// downloaded zip before unpacking, and the manager verifies the payload.
var Builtin = Pin{
	Artifact:   GuestArtifact,
	Commit:     "3ff150fdbd27b7cab46582eee4f738230aafe6ee",
	RunID:      41594421666,
	ArtifactID: 13212080359,
	SHA256:     "cfb5fcda8e346b1f53c437d02ec181d48a777729d7798d4d76893328eecc9d64",
	Size:       29997755,
	Expires:    time.Date(2027, 2, 1, 7, 9, 15, 0, time.UTC),
	Lite: LitePin{
		Artifact: LiteArtifact,
		Commit:   "97267425669943e9ced5691ee8a311543110a258",
		RunID:    41582282341,
		SHA256:   "0bed5a806f52c71bc2b45f94ed08da3758d22084bc897578ee898673721847c3",
		Size:     50118510,
		Expires:  time.Date(2027, 2, 1, 5, 9, 59, 0, time.UTC),
	},
}

// ManifestURL serves the current pin as JSON, kept on its own branch so that
// every amdl release reads the same file.
const ManifestURL = "https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/wrapper-pins/wrapper-manager-guest.json"

var hexSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (p Pin) valid(now time.Time) error {
	switch {
	case p.Artifact != GuestArtifact || p.RunID <= 0 || p.Commit == "" || p.ArtifactID <= 0 || !hexSHA.MatchString(p.SHA256) || p.Size <= 0:
		return fmt.Errorf("pin for artifact %d is incomplete", p.ArtifactID)
	case !p.Expires.IsZero() && now.After(p.Expires):
		return fmt.Errorf("artifact %d expired on %s", p.ArtifactID, p.Expires.Format("2006-01-02"))
	}
	return p.Lite.valid(now)
}

// manifestTimeout bounds the manifest read; the builtin pin is the fallback.
const manifestTimeout = 10 * time.Second

// CurrentPin returns the published pin when ManifestURL answers with a valid
// one, else Builtin when it has not expired, saying which.
func CurrentPin(ctx context.Context, c *http.Client, now time.Time) (Pin, string, error) {
	if p, err := fetchPin(ctx, c); err == nil && p.valid(now) == nil {
		return p, "manifest", nil
	}
	if err := Builtin.valid(now); err != nil {
		return Pin{}, "", fmt.Errorf("no current wrapper download: the built-in %w and %s did not give a newer one; "+
			"download the manager-qemu-guest artifact yourself and run amdl wrapper import <zip>", err, ManifestURL)
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
