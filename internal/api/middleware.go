package api

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"amdl/internal/logging"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

func (s *Server) observeHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !requestIDPattern.MatchString(requestID) {
			requestID = newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)
		logger := s.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger = logger.With("component", "api", "request_id", requestID)
		r = r.WithContext(logging.NewContext(r.Context(), logger))
		tracked := &trackingResponseWriter{ResponseWriter: w}

		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("http request panic", "method", r.Method, "path", r.URL.Path, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
				if !tracked.wroteHeader {
					writeError(tracked, http.StatusInternalServerError, CodeInternalError, "internal server error")
				} else {
					panic(recovered)
				}
			}
			if s.currentConfig().Logging.AccessLog {
				status := tracked.status
				if status == 0 {
					status = http.StatusOK
				}
				level := slog.LevelInfo
				if status >= 500 {
					level = slog.LevelError
				} else if status >= 400 {
					level = slog.LevelWarn
				}
				logger.Log(r.Context(), level, "http request",
					"method", r.Method, "path", r.URL.Path, "route", r.Pattern,
					"status", status, "bytes", tracked.bytes,
					"duration_ms", time.Since(started).Milliseconds(), "remote_ip", remoteIP(r.RemoteAddr),
				)
			}
		}()

		next.ServeHTTP(tracked, r)
	})
}

func newRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err == nil {
		return hex.EncodeToString(raw)
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func remoteIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err == nil {
		return host
	}
	return address
}

type trackingResponseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (w *trackingResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *trackingResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	return n, err
}

func (w *trackingResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *trackingResponseWriter) Flush() { _ = w.FlushError() }

// FlushError lets http.ResponseController report a failed flush, which is how
// a stream notices that its client went away.
func (w *trackingResponseWriter) FlushError() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *trackingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w *trackingResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func (w *trackingResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if rf, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := rf.ReadFrom(reader)
		w.bytes += n
		return n, err
	}
	n, err := io.Copy(struct{ io.Writer }{w.ResponseWriter}, reader)
	w.bytes += n
	return n, err
}
