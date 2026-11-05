package ui

import (
	"bufio"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// sceneNet counts the demo's answers still being read, so that every
// answer to a request one frame made is in before the next: the films come
// out the same on a busy host and an idle one.
// It also gives the demo's names the scene's renames, in every answer.
type sceneNet struct {
	rt       http.RoundTripper
	rename   map[string]string
	inflight atomic.Int32
}

func (n *sceneNet) RoundTrip(req *http.Request) (*http.Response, error) {
	n.inflight.Add(1)
	resp, err := n.rt.RoundTrip(req)
	if err != nil || resp.Body == nil {
		n.inflight.Add(-1)
		return resp, err
	}
	feed := strings.HasSuffix(req.URL.Path, "/events")
	if len(n.rename) > 0 {
		resp.Body = &renamed{r: bufio.NewReader(resp.Body), c: resp.Body, rename: n.rename}
		resp.ContentLength = -1
		resp.Header.Del("Content-Length")
	}
	// A feed stays open for good.
	if feed {
		n.inflight.Add(-1)
		return resp, nil
	}
	resp.Body = &sceneBody{ReadCloser: resp.Body, n: n}
	return resp, nil
}

// renamed is an answer with names replaced, line by line: an event of
// the feed is a line.
type renamed struct {
	r      *bufio.Reader
	c      io.Closer
	rename map[string]string
	buf    []byte
}

func (b *renamed) Read(p []byte) (int, error) {
	for len(b.buf) == 0 {
		line, err := b.r.ReadString('\n')
		for from, to := range b.rename {
			line = strings.ReplaceAll(line, from, to)
		}
		b.buf = []byte(line)
		if err != nil && len(b.buf) == 0 {
			return 0, err
		}
		if err != nil {
			break
		}
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	return n, nil
}

func (b *renamed) Close() error { return b.c.Close() }

type sceneBody struct {
	io.ReadCloser
	n    *sceneNet
	once sync.Once
}

func (b *sceneBody) Close() error {
	b.once.Do(func() { b.n.inflight.Add(-1) })
	return b.ReadCloser.Close()
}
