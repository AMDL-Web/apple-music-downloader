package wrapperhost

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RangeFetcher is a Fetcher that can also serve byte ranges of the artifact,
// which lets Download run several connections at once.
type RangeFetcher interface {
	Fetcher
	// Ranges prepares a download of p. A failure that reaches the caller is
	// one retrying will not cure.
	Ranges(ctx context.Context, p Pin) (RangeSource, error)
}

// RangeSource serves the byte ranges of one artifact. Range is safe to call
// from several goroutines.
type RangeSource interface {
	// Conns is how many ranges to fetch at once.
	Conns() int
	// Range writes the bytes [start, end) to w. A server that ignores Range
	// gives an error that satisfies errors.Is(err, ErrNoRange).
	Range(ctx context.Context, start, end int64, w io.Writer) error
	Close()
}

// ErrNoRange means the server answered a Range request with the whole file.
var ErrNoRange = errors.New("the server does not honor Range requests")

// permanent marks an error that retrying cannot cure.
type permanent struct{ err error }

func (e permanent) Error() string { return e.err.Error() }
func (e permanent) Unwrap() error { return e.err }

// statusError is an unexpected HTTP status, named by host only: a blob URL
// carries a signature.
type statusError struct {
	host, status string
	code         int
}

func (e *statusError) Error() string { return e.host + ": " + e.status }

// defaultConns is how many connections a download uses. nightly.link hands
// out a signed Azure blob URL that serves one connection at about 25 KB/s,
// and eight connections measured eight times that, so the limit is per
// connection; eight stays clear of the six to sixteen that browsers and
// download managers open to one host.
const defaultConns = 8

// resolveTimeout bounds one nightly.link lookup.
const resolveTimeout = 30 * time.Second

func (n NightlyLink) artifactURL(p Pin) string {
	base := n.Base
	if base == "" {
		base = "https://nightly.link"
	}
	return fmt.Sprintf("%s/WorldObservationLog/wrapper/actions/artifacts/%d.zip", base, p.ArtifactID)
}

// Ranges implements RangeFetcher. It resolves nightly.link's redirect once;
// the ranges then go straight to the blob host, and a range refused as
// expired resolves again.
func (n NightlyLink) Ranges(ctx context.Context, p Pin) (RangeSource, error) {
	conns := n.Conns
	if conns <= 0 {
		conns = defaultConns
	}
	client := http.DefaultClient
	if n.Client != nil {
		client = n.Client
	}
	s := &nightlySource{src: n.artifactURL(p), id: p.ArtifactID, size: p.Size, conns: conns}
	s.resolver = *client
	s.resolver.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s.client = client
	if client.Transport == nil {
		// One idle connection per worker, and no HTTP/2: it would carry every
		// range over a single connection, and the limit is per connection.
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.MaxIdleConnsPerHost = conns
		t.ForceAttemptHTTP2 = false
		t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		c := *client
		c.Transport = t
		s.client, s.transport = &c, t
	}
	if err := retry(ctx, func() error { _, err := s.refresh(ctx, ""); return err }); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

type nightlySource struct {
	src       string
	id, size  int64
	conns     int
	resolver  http.Client
	client    *http.Client
	transport *http.Transport

	mu  sync.Mutex
	url string
}

func (s *nightlySource) Conns() int { return s.conns }

func (s *nightlySource) Close() {
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}
}

// refresh returns the blob URL, resolving it again unless another worker did
// since stale was handed out.
func (s *nightlySource) refresh(ctx context.Context, stale string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.url != "" && s.url != stale {
		return s.url, nil
	}
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.src, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.resolver.Do(req)
	if err != nil {
		return "", redact(err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		loc, err := resp.Location()
		if err != nil {
			return "", err
		}
		s.url = loc.String()
	case resp.StatusCode == http.StatusOK:
		s.url = s.src
	default:
		return "", s.statusErr(req.URL, resp)
	}
	return s.url, nil
}

func (s *nightlySource) statusErr(u *url.URL, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusNotFound:
		return permanent{fmt.Errorf("artifact %d is gone from GitHub (artifacts expire 90 days after their build)", s.id)}
	case http.StatusRequestedRangeNotSatisfiable:
		return permanent{fmt.Errorf("%s: %s: the pin's size %d does not fit the artifact", u.Host, resp.Status, s.size)}
	}
	return &statusError{host: u.Host, status: resp.Status, code: resp.StatusCode}
}

// redact keeps a signed URL out of an error.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		host := ue.URL
		if u, perr := url.Parse(ue.URL); perr == nil {
			host = u.Host
		}
		return fmt.Errorf("%s %s: %w", ue.Op, host, ue.Err)
	}
	return err
}

// Range implements RangeSource.
func (s *nightlySource) Range(ctx context.Context, start, end int64, w io.Writer) error {
	s.mu.Lock()
	u := s.url
	s.mu.Unlock()
	err := s.get(ctx, u, start, end, w)
	var se *statusError
	if errors.As(err, &se) && (se.code == http.StatusForbidden || se.code == http.StatusUnauthorized) {
		// A signed URL expires; ask nightly.link for a fresh one.
		fresh, rerr := s.refresh(ctx, u)
		if rerr != nil {
			return rerr
		}
		err = s.get(ctx, fresh, start, end, w)
	}
	return err
}

func (s *nightlySource) get(ctx context.Context, u string, start, end int64, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return redact(err)
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(start, 10)+"-"+strconv.FormatInt(end-1, 10))
	resp, err := s.client.Do(req)
	if err != nil {
		return redact(err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		return permanent{ErrNoRange}
	default:
		return s.statusErr(req.URL, resp)
	}
	if first, total, ok := parseContentRange(resp.Header.Get("Content-Range")); ok {
		switch {
		case first != start:
			return permanent{ErrNoRange}
		case total != s.size:
			return permanent{fmt.Errorf("the artifact is %d bytes but the pin says %d", total, s.size)}
		}
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, end-start))
	if err == nil && n != end-start {
		err = io.ErrUnexpectedEOF
	}
	return err
}

// parseContentRange reads "bytes first-last/total".
func parseContentRange(h string) (first, total int64, ok bool) {
	rest, found := strings.CutPrefix(h, "bytes ")
	if !found {
		return 0, 0, false
	}
	span, tot, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, false
	}
	a, _, found := strings.Cut(span, "-")
	if !found {
		return 0, 0, false
	}
	first, err1 := strconv.ParseInt(a, 10, 64)
	total, err2 := strconv.ParseInt(tot, 10, 64)
	return first, total, err1 == nil && err2 == nil
}
