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

// Pin names one qemu-assets artifact of upstream's build-lite workflow and
// what its zip must hash to. GitHub deletes artifacts 90 days after a run, so
// a pin built into a release goes stale; ManifestURL serves a current one.
type Pin struct {
	Commit     string    `json:"commit"`
	RunID      int64     `json:"run_id"`
	ArtifactID int64     `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	Expires    time.Time `json:"expires_at"`
}

// Builtin is the pin this binary was released with: run 40194181305 of
// build-lite on lite at bde3335. Its sha256 is the artifact's digest from the
// GitHub API, which matched the bytes nightly.link served for it.
var Builtin = Pin{
	Commit:     "bde3335f47a30518e1f53f79cbae94bf76b27fb3",
	RunID:      40194181305,
	ArtifactID: 12598758939,
	SHA256:     "d2c150c07e20a42d0978420f2d43a1a7b43c1e843575ff48075ef89940aef292",
	Size:       64451213,
	Expires:    time.Date(2027, 1, 23, 12, 40, 50, 0, time.UTC),
}

// ManifestURL serves the current pin as JSON, kept on its own branch so that
// every amdl release reads the same file.
const ManifestURL = "https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/wrapper-pins/wrapper-pin.json"

var hexSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (p Pin) valid(now time.Time) error {
	switch {
	case p.ArtifactID <= 0 || !hexSHA.MatchString(p.SHA256) || p.Size <= 0:
		return fmt.Errorf("pin for artifact %d is incomplete", p.ArtifactID)
	case !p.Expires.IsZero() && now.After(p.Expires):
		return fmt.Errorf("artifact %d expired on %s", p.ArtifactID, p.Expires.Format("2006-01-02"))
	}
	return nil
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
			"download the qemu-assets artifact yourself and run amdl wrapper import <zip>", err, ManifestURL)
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
