package media

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"amdl/internal/limits"
)

const (
	// HLS playlists are text metadata, never media payloads. This is generous
	// for even very large variant/segment lists while bounding a bad upstream.
	maxPlaylistBytes int64 = 16 << 20
	// AAC-LC is currently materialized in memory before MP4 processing. Bound
	// both advertised and streamed sizes so a forged Content-Length or endless
	// response cannot exhaust the process. 512 MiB still covers multi-hour AAC.
	maxInMemoryMediaBytes int64 = 512 << 20
)

func downloadText(ctx context.Context, client *http.Client, uri string, gates ...*limits.RequestGate) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return "", err
	}
	var resp *http.Response
	if len(gates) > 0 && gates[0] != nil {
		resp, err = gates[0].DoWith429Retry(ctx, client, req, false, limits.DefaultRetryDelay)
	} else {
		resp, err = client.Do(req)
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("download %s failed: %s", uri, resp.Status)
	}
	raw, err := readAllLimited(resp.Body, maxPlaylistBytes)
	return string(raw), err
}

// downloadBytes fetches uri into memory without an internal reconnect. AAC-LC
// uses this path and leaves retry/backoff to its caller.
func downloadBytes(ctx context.Context, client *http.Client, uri string, onProgress func(float64)) ([]byte, error) {
	return downloadBytesInternal(ctx, client, uri, false, onProgress)
}

// downloadBytesWithRangeResume fetches uri into memory. If a response body is
// interrupted after making progress and the server supplied a usable If-Range
// validator, one transparent Range/If-Range reconnect preserves the bytes
// already held in memory. The reconnect is deliberately bounded to one request;
// the caller's outer retry policy remains responsible for backoff and further
// attempts.
//
// onProgress, if non-nil, is called periodically with a value in [0,1]
// representing cumulative download progress. If the total size is unknown the
// callback receives -1.
func downloadBytesWithRangeResume(ctx context.Context, client *http.Client, uri string, onProgress func(float64)) ([]byte, error) {
	return downloadBytesInternal(ctx, client, uri, true, onProgress)
}

func downloadBytesInternal(ctx context.Context, client *http.Client, uri string, allowRangeResume bool, onProgress func(float64)) ([]byte, error) {
	var buf bytes.Buffer
	var metadata resumeMetadata
	var total int64
	resumeAttempted := false
	chunk := make([]byte, 32*1024)

	for {
		offset := int64(buf.Len())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			req.Header.Set("If-Range", resumeValidator(metadata))
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		rangeStart, rangeEnd := int64(0), int64(-1)
		switch {
		case offset > 0 && resp.StatusCode == http.StatusPartialContent:
			start, end, parsedTotal, ok := parseContentRange(resp.Header.Get("Content-Range"))
			expectedBody := end - start + 1
			if !ok || start != offset || (resp.ContentLength >= 0 && resp.ContentLength != expectedBody) ||
				(metadata.Total > 0 && metadata.Total != parsedTotal) || resumeObjectChanged(metadata, resp.Header) {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("download %s returned invalid resume range %q", uri, resp.Header.Get("Content-Range"))
			}
			total = parsedTotal
			rangeStart, rangeEnd = start, end
		case offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
			parsedTotal, ok := parseUnsatisfiedContentRange(resp.Header.Get("Content-Range"))
			_ = resp.Body.Close()
			if ok && parsedTotal == offset && (metadata.Total <= 0 || metadata.Total == parsedTotal) &&
				!resumeObjectChanged(metadata, resp.Header) {
				if onProgress != nil {
					onProgress(1)
				}
				return buf.Bytes(), nil
			}
			return nil, fmt.Errorf("download %s could not resume at byte %d", uri, offset)
		case offset > 0 && resp.StatusCode == http.StatusOK:
			// Range was ignored or If-Range did not match. Reuse the full response,
			// but discard the old prefix so representations can never be mixed.
			buf.Reset()
			offset = 0
			metadata = resumeMetadata{}
			total = responseContentLength(resp.Header)
		case offset > 0:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("download %s range request failed: %s", uri, resp.Status)
		case resp.StatusCode == http.StatusPartialContent:
			start, end, parsedTotal, ok := parseContentRange(resp.Header.Get("Content-Range"))
			expectedBody := end - start + 1
			if !ok || start != 0 || (resp.ContentLength >= 0 && resp.ContentLength != expectedBody) {
				_ = resp.Body.Close()
				return nil, fmt.Errorf("download %s returned invalid Content-Range %q", uri, resp.Header.Get("Content-Range"))
			}
			total = parsedTotal
			rangeStart, rangeEnd = start, end
		case resp.StatusCode >= 300:
			_ = resp.Body.Close()
			return nil, fmt.Errorf("download %s failed: %s", uri, resp.Status)
		default:
			total = responseContentLength(resp.Header)
		}

		if total > maxInMemoryMediaBytes {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("download %s is too large for in-memory processing: %d bytes (max %d)", uri, total, maxInMemoryMediaBytes)
		}
		metadata = resumeMetadata{
			ETag:         firstNonEmpty(resp.Header.Get("ETag"), metadata.ETag),
			LastModified: firstNonEmpty(resp.Header.Get("Last-Modified"), metadata.LastModified),
			Total:        total,
		}
		if total > int64(buf.Cap()) {
			buf.Grow(int(total) - buf.Len())
		}
		if onProgress != nil {
			if total > 0 {
				onProgress(min(1, float64(offset)/float64(total)))
			} else {
				onProgress(-1)
			}
		}

		responseStart := int64(buf.Len())
		var transferErr error
		resumeEligible := true
		for {
			n, readErr := resp.Body.Read(chunk)
			if n > 0 {
				if int64(buf.Len())+int64(n) > maxInMemoryMediaBytes {
					transferErr = fmt.Errorf("download %s exceeded in-memory limit of %d bytes", uri, maxInMemoryMediaBytes)
					resumeEligible = false
					break
				}
				_, _ = buf.Write(chunk[:n])
				if onProgress != nil && total > 0 {
					onProgress(min(1, float64(buf.Len())/float64(total)))
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				transferErr = readErr
				break
			}
		}
		_ = resp.Body.Close()

		if transferErr == nil && rangeEnd >= 0 && int64(buf.Len())-responseStart != rangeEnd-rangeStart+1 {
			transferErr = fmt.Errorf("download %s returned %d range bytes, want %d", uri, int64(buf.Len())-responseStart, rangeEnd-rangeStart+1)
		}
		if transferErr == nil && total > 0 && int64(buf.Len()) != total {
			transferErr = fmt.Errorf("download %s ended at %d of %d bytes", uri, buf.Len(), total)
		}
		if transferErr == nil {
			return buf.Bytes(), nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// A naked Range could splice two objects at the same URL. Only preserve
		// the in-memory prefix when If-Range can bind the follow-up request.
		if !allowRangeResume || !resumeEligible || resumeAttempted || buf.Len() == 0 || resumeValidator(metadata) == "" {
			return nil, transferErr
		}
		resumeAttempted = true
	}
}

func readAllLimited(r io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("response exceeded limit of %d bytes", limit)
	}
	return raw, nil
}

// downloadToFile streams uri into a stable checkpoint under dir. resumeKey is
// the final output path, which is already protected by the downloader's output
// lock, so retries and a recovered job resolve to the same checkpoint without
// allowing two tracks to append concurrently. A failed transfer deliberately
// leaves both the partial media and its small metadata sidecar in place.
//
// When a checkpoint exists, the request uses Range and, when the server supplied
// one, If-Range. A server that ignores the range or reports a changed validator
// causes a safe full restart instead of mixing bytes from different objects.
// The caller must call cleanupResumableDownload after the encrypted media is no
// longer needed.
func downloadToFile(ctx context.Context, client *http.Client, uri, dir, owner, resumeKey string, onProgress func(float64)) (string, error) {
	path, metadataPath := resumableDownloadPaths(dir, owner, resumeKey)
	offset, metadata := loadResumeCheckpoint(path, metadataPath, uri)

	// One retry is sufficient here: it is used only when a stale/invalid Range
	// response tells us to discard the checkpoint and issue a clean GET.
	for restart := 0; restart < 2; restart++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept-Encoding", "identity")
		if offset > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if validator := resumeValidator(metadata); validator != "" {
				req.Header.Set("If-Range", validator)
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}

		restartClean := func() {
			_ = resp.Body.Close()
			cleanupResumableDownload(path)
			offset = 0
			metadata = resumeMetadata{}
		}

		var total, rangeStart, rangeEnd int64
		rangeEnd = -1
		switch {
		case offset > 0 && resp.StatusCode == http.StatusPartialContent:
			start, end, parsedTotal, ok := parseContentRange(resp.Header.Get("Content-Range"))
			expectedBody := end - start + 1
			if !ok || start != offset || (resp.ContentLength >= 0 && resp.ContentLength != expectedBody) ||
				(metadata.Total > 0 && metadata.Total != parsedTotal) || resumeObjectChanged(metadata, resp.Header) {
				restartClean()
				continue
			}
			total = parsedTotal
			rangeStart, rangeEnd = start, end
		case offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
			parsedTotal, ok := parseUnsatisfiedContentRange(resp.Header.Get("Content-Range"))
			_ = resp.Body.Close()
			if ok && parsedTotal == offset && metadata.Complete && !resumeObjectChanged(metadata, resp.Header) {
				metadata.Total = parsedTotal
				if err := writeResumeMetadata(metadataPath, metadata); err != nil {
					return "", err
				}
				if onProgress != nil {
					onProgress(1)
				}
				return path, nil
			}
			cleanupResumableDownload(path)
			offset = 0
			metadata = resumeMetadata{}
			continue
		case offset > 0 && resp.StatusCode == http.StatusOK:
			// Range was ignored or If-Range did not match. Reuse this full body,
			// but truncate the old checkpoint before writing it.
			offset = 0
			metadata = resumeMetadata{}
			total = responseContentLength(resp.Header)
		case offset > 0:
			_ = resp.Body.Close()
			return "", fmt.Errorf("download %s range request failed: %s", uri, resp.Status)
		case resp.StatusCode == http.StatusPartialContent:
			start, end, parsedTotal, ok := parseContentRange(resp.Header.Get("Content-Range"))
			expectedBody := end - start + 1
			if !ok || start != 0 || (resp.ContentLength >= 0 && resp.ContentLength != expectedBody) {
				_ = resp.Body.Close()
				return "", fmt.Errorf("download %s returned invalid Content-Range %q", uri, resp.Header.Get("Content-Range"))
			}
			total = parsedTotal
			rangeStart, rangeEnd = start, end
		case resp.StatusCode >= 300:
			_ = resp.Body.Close()
			return "", fmt.Errorf("download %s failed: %s", uri, resp.Status)
		default:
			total = responseContentLength(resp.Header)
		}

		flags := os.O_CREATE | os.O_WRONLY
		if offset > 0 {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			_ = resp.Body.Close()
			return "", err
		}
		f, err := os.OpenFile(path, flags, 0o600)
		if err != nil {
			_ = resp.Body.Close()
			return "", err
		}

		metadata = resumeMetadata{
			Version:      resumeMetadataVersion,
			SourceHash:   sourceFingerprint(uri),
			ETag:         firstNonEmpty(resp.Header.Get("ETag"), metadata.ETag),
			LastModified: firstNonEmpty(resp.Header.Get("Last-Modified"), metadata.LastModified),
			Total:        total,
			Complete:     false,
		}
		if err := writeResumeMetadata(metadataPath, metadata); err != nil {
			_ = f.Close()
			_ = resp.Body.Close()
			return "", err
		}

		downloaded := offset
		if onProgress != nil {
			if total > 0 {
				onProgress(min(1, float64(downloaded)/float64(total)))
			} else {
				onProgress(-1)
			}
		}
		chunk := make([]byte, 32*1024)
		var transferErr error
		for {
			n, readErr := resp.Body.Read(chunk)
			if n > 0 {
				if _, writeErr := f.Write(chunk[:n]); writeErr != nil {
					transferErr = writeErr
					break
				}
				downloaded += int64(n)
				if onProgress != nil && total > 0 {
					onProgress(min(1, float64(downloaded)/float64(total)))
				}
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				transferErr = readErr
				break
			}
		}
		_ = resp.Body.Close()
		if syncErr := f.Sync(); transferErr == nil {
			transferErr = syncErr
		}
		if closeErr := f.Close(); transferErr == nil {
			transferErr = closeErr
		}
		if transferErr != nil {
			return "", transferErr
		}
		if rangeEnd >= 0 && downloaded-offset != rangeEnd-rangeStart+1 {
			return "", fmt.Errorf("download %s returned %d range bytes, want %d", uri, downloaded-offset, rangeEnd-rangeStart+1)
		}
		if total > 0 && downloaded != total {
			return "", fmt.Errorf("download %s ended at %d of %d bytes", uri, downloaded, total)
		}
		if total <= 0 {
			metadata.Total = downloaded
		}
		metadata.Complete = true
		if err := writeResumeMetadata(metadataPath, metadata); err != nil {
			return "", err
		}
		return path, nil
	}
	return "", fmt.Errorf("download %s returned an invalid range response", uri)
}

func responseContentLength(header http.Header) int64 {
	total, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)
	if total <= 0 {
		total, _ = strconv.ParseInt(header.Get("X-Apple-MS-Content-Length"), 10, 64)
	}
	return total
}

func parseContentRange(value string) (start, end, total int64, ok bool) {
	if _, err := fmt.Sscanf(value, "bytes %d-%d/%d", &start, &end, &total); err != nil || start < 0 || end < start || total <= end {
		return 0, 0, 0, false
	}
	return start, end, total, true
}

func parseUnsatisfiedContentRange(value string) (int64, bool) {
	var total int64
	if _, err := fmt.Sscanf(value, "bytes */%d", &total); err != nil || total < 0 {
		return 0, false
	}
	return total, true
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 90 * time.Second}
}

var _ = strconv.Itoa
