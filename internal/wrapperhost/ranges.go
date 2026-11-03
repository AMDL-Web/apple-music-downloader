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

// RangeFetcher is a Fetcher that can also serve byte ranges of the asset,
// which lets Download run several connections at once.
type RangeFetcher interface {
	Fetcher
	// Ranges prepares a download of a. A failure that reaches the caller is
	// one retrying will not cure.
	Ranges(ctx context.Context, a Asset) (RangeSource, error)
}

// RangeSource serves the byte ranges of one asset. Range is safe to call
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

// statusError is an unexpected HTTP status, named by host only: a CDN URL
// carries a signature.
type statusError struct {
	host, status string
	code         int
}

func (e *statusError) Error() string { return e.host + ": " + e.status }

// defaultConns is how many connections a download uses. GitHub's release CDN
// measured 6.5 MB/s on one connection from China; a few more ride out one
// that stalls, and stay clear of the six to sixteen that browsers and
// download managers open to one host.
const defaultConns = 4

// resolveTimeout bounds one redirect lookup.
const resolveTimeout = 30 * time.Second

// Ranges implements RangeFetcher. It resolves GitHub's redirect once; the
// ranges then go straight to the CDN, and a range refused as expired
// resolves again.
func (r Releases) Ranges(ctx context.Context, a Asset) (RangeSource, error) {
	conns := r.Conns
	if conns <= 0 {
		conns = defaultConns
	}
	client := r.client()
	s := &assetSource{src: r.url(a), size: a.Size, conns: conns}
	s.resolver = *client
	s.resolver.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	s.client = client
	if client.Transport == nil {
		// One idle connection per worker, and no HTTP/2: it would carry every
		// range over a single connection. The TLS handshake still offers
		// http/1.1 by ALPN: github.com closes one that offers nothing.
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.MaxIdleConnsPerHost = conns
		t.ForceAttemptHTTP2 = false
		t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		t.TLSClientConfig = &tls.Config{NextProtos: []string{"http/1.1"}}
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

type assetSource struct {
	src       string
	size      int64
	conns     int
	resolver  http.Client
	client    *http.Client
	transport *http.Transport

	mu  sync.Mutex
	url string
}

func (s *assetSource) Conns() int { return s.conns }

func (s *assetSource) Close() {
	if s.transport != nil {
		s.transport.CloseIdleConnections()
	}
}

// refresh returns the CDN URL, resolving it again unless another worker did
// since stale was handed out.
func (s *assetSource) refresh(ctx context.Context, stale string) (string, error) {
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

func (s *assetSource) statusErr(u *url.URL, resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusNotFound:
		return permanent{fmt.Errorf("%s is not published", s.src)}
	case http.StatusRequestedRangeNotSatisfiable:
		return permanent{fmt.Errorf("%s: %s: the pin's size %d does not fit the asset", u.Host, resp.Status, s.size)}
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
func (s *assetSource) Range(ctx context.Context, start, end int64, w io.Writer) error {
	s.mu.Lock()
	u := s.url
	s.mu.Unlock()
	err := s.get(ctx, u, start, end, w)
	var se *statusError
	if errors.As(err, &se) && (se.code == http.StatusForbidden || se.code == http.StatusUnauthorized) {
		// A signed URL expires; ask GitHub for a fresh one.
		fresh, rerr := s.refresh(ctx, u)
		if rerr != nil {
			return rerr
		}
		err = s.get(ctx, fresh, start, end, w)
	}
	return err
}

func (s *assetSource) get(ctx context.Context, u string, start, end int64, w io.Writer) error {
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
			return permanent{fmt.Errorf("the asset is %d bytes but the pin says %d", total, s.size)}
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
