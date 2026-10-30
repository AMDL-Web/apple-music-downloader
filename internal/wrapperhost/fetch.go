package wrapperhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Fetcher writes a pinned artifact's bytes from offset on, so an interrupted
// download resumes. Download checks what it wrote against the pin.
type Fetcher interface {
	Fetch(ctx context.Context, p Pin, offset int64, w io.Writer) error
}

// NightlyLink fetches through nightly.link, which serves a public repository's
// Actions artifacts without a GitHub sign-in (GitHub's own artifact URL
// answers 401 to anonymous requests). The URL names the artifact by id, so it
// cannot drift to a newer build; the sha256 check guards the bytes.
type NightlyLink struct{ Client *http.Client }

// fetchTimeout bounds one download attempt; a slow link resumes on the next.
const fetchTimeout = 30 * time.Minute

// Fetch implements Fetcher.
func (n NightlyLink) Fetch(ctx context.Context, p Pin, offset int64, w io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	url := fmt.Sprintf("https://nightly.link/WorldObservationLog/wrapper/actions/artifacts/%d.zip", p.ArtifactID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := n.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case offset > 0 && resp.StatusCode == http.StatusPartialContent, offset == 0 && resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("artifact %d is gone from GitHub (artifacts expire 90 days after their build)", p.ArtifactID)
	default:
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// Download fetches p into path, resuming a partial file left there, and fails
// unless the whole file has p's size and sha256.
func Download(ctx context.Context, f Fetcher, p Pin, path string) error {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return err
	}
	if st.Size() > p.Size {
		if err := file.Truncate(0); err != nil {
			return err
		}
	}
	if st.Size() < p.Size {
		if err := f.Fetch(ctx, p, min(st.Size(), p.Size), file); err != nil {
			return err
		}
	}
	return verify(file, p)
}

func verify(file *os.File, p Pin) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(h, file)
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); n != p.Size || got != p.SHA256 {
		_ = file.Truncate(0) // a bad download is not worth resuming
		return fmt.Errorf("%w: got %d bytes with sha256 %s, want %d bytes with %s", ErrChecksum, n, got, p.Size, p.SHA256)
	}
	return nil
}

// ErrChecksum means a download did not match its pin.
var ErrChecksum = errors.New("the download does not match its pin")
