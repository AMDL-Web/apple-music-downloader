package wrapperhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

// payloadDir holds the pinned wrapper-lite payload zip. The guest's manager
// installs the payload from the host instead of downloading it itself: a
// download inside the guest has no progress, cannot resume and, through
// QEMU's user-mode network on an emulated CPU, is slow.
const payloadDir = "payload"

// payloadPath is where the payload l pins is kept.
func (h Home) payloadPath(l LitePin) string {
	return h.path(payloadDir, l.Artifact+"-"+l.SHA256[:12]+".zip")
}

// HasPayload reports whether the payload l pins is here at its full size;
// FetchPayload checks its sha256.
func (h Home) HasPayload(l LitePin) bool {
	if l.complete() != nil {
		return false
	}
	st, err := os.Stat(h.payloadPath(l))
	return err == nil && st.Size() == l.Size
}

// FetchPayload downloads the payload l pins, resuming a partial one, and
// checks it against l; one already here is only checked. Payloads of other
// pins are removed once it is in place.
func (h Home) FetchPayload(ctx context.Context, f Fetcher, l LitePin, report func(Progress)) error {
	if err := l.complete(); err != nil {
		return err
	}
	if err := os.MkdirAll(h.path(payloadDir), 0o700); err != nil {
		return err
	}
	path := h.payloadPath(l)
	if err := Download(ctx, f, l.Asset(), path, report); err != nil {
		return fmt.Errorf("wrapper-lite payload: %w", err)
	}
	entries, _ := os.ReadDir(h.path(payloadDir)) // listing only prunes
	for _, e := range entries {
		if p := h.path(payloadDir, e.Name()); p != path && p != path+".parts" {
			_ = os.RemoveAll(p) // another pin's payload
		}
	}
	return nil
}

// payloadServer serves one payload zip to the guest from the host's loopback,
// which QEMU's user-mode network shows the guest as guestHost. It lives as
// long as the guest: the manager installs the payload whenever its disk
// lacks it. The path carries a random token, and the file is public and
// checked by its sha256 on both ends.
type payloadServer struct {
	srv *http.Server
	// URL is the payload's address as the guest sees it.
	URL string
}

func servePayload(path string, l LitePin) (*payloadServer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	f.Close()
	if err != nil {
		return nil, err
	}
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	name := "/" + hex.EncodeToString(token[:]) + "/" + l.Artifact + ".zip"
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+name, func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(path)
		if err != nil {
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, l.Artifact+".zip", st.ModTime(), f)
	})
	s := &payloadServer{srv: &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}}
	s.URL = fmt.Sprintf("http://%s:%d%s", guestHost, ln.Addr().(*net.TCPAddr).Port, name)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			_ = err // the guest's manager retries, and the boot times out
		}
	}()
	return s, nil
}

// Close stops serving.
func (s *payloadServer) Close() { _ = s.srv.Close() }
