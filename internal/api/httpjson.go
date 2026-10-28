package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxJSONBodyBytes bounds every JSON request body handled by this API. The
// largest legitimate payload is a runtime-config patch or a batch of 100
// URLs; 1 MiB leaves ample room for both while preventing unbounded reads.
const maxJSONBodyBytes int64 = 1 << 20

var errTrailingJSON = errors.New("request body must contain exactly one JSON value")

// decodeJSONBody applies the same resource and framing rules to every JSON
// endpoint: at most maxJSONBodyBytes, exactly one JSON value, and optionally
// no unknown object fields. It writes the request error response itself so a
// MaxBytesError can consistently map to 413 instead of being flattened into
// a malformed-JSON 400.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, disallowUnknownFields bool) bool {
	if r.ContentLength > maxJSONBodyBytes {
		writeError(w, http.StatusRequestEntityTooLarge, CodeRequestTooLarge, fmt.Sprintf("JSON request body exceeds %d bytes", maxJSONBodyBytes))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if disallowUnknownFields {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(dst); err != nil {
		writeJSONBodyError(w, err)
		return false
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errTrailingJSON
		}
		writeJSONBodyError(w, err)
		return false
	}
	return true
}

func writeJSONBodyError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, CodeRequestTooLarge, fmt.Sprintf("JSON request body exceeds %d bytes", tooLarge.Limit))
		return
	}
	if errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, CodeInvalidJSON, "request body is empty")
		return
	}
	writeError(w, http.StatusBadRequest, jsonErrorCode(err), err.Error())
}

// jsonErrorCode classifies a decode error: a body that is not well-formed JSON
// is invalid_json; well-formed JSON with the wrong shape (an unknown field, a
// wrong type) is invalid_request.
func jsonErrorCode(err error) ErrorCode {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, errTrailingJSON) {
		return CodeInvalidJSON
	}
	return CodeInvalidRequest
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
