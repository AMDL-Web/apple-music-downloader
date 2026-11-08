package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
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
	writeError(w, http.StatusBadRequest, jsonErrorCode(err), describeJSONError(err))
}

// describeJSONError says what was wrong with a body that decoded into the
// wrong type in the request's own terms ("download.max_attempts must be a
// number, got a string") instead of naming Go types; other errors keep
// their text.
func describeJSONError(err error) string {
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) || typeErr.Type == nil {
		return err.Error()
	}
	field := typeErr.Field
	if field == "" {
		field = "the body"
	}
	return fmt.Sprintf("%s must be %s, got %s", field, jsonKind(typeErr.Type.Kind()), jsonValueKind(typeErr.Value))
}

func jsonKind(k reflect.Kind) string {
	switch k {
	case reflect.Slice, reflect.Array:
		return "a list"
	case reflect.Map, reflect.Struct:
		return "an object"
	case reflect.Bool:
		return "true or false"
	case reflect.String:
		return "a string"
	case reflect.Float32, reflect.Float64:
		return "a number"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "a whole number"
	}
	return "a " + k.String()
}

// jsonValueKind names UnmarshalTypeError.Value, which is "string",
// "number", "bool", "array", "object" or, for a number that does not fit,
// "number <text>".
func jsonValueKind(v string) string {
	switch {
	case v == "array":
		return "a list"
	case v == "object":
		return "an object"
	case v == "bool":
		return "true or false"
	case strings.HasPrefix(v, "number "):
		return strings.TrimPrefix(v, "number ")
	}
	return "a " + v
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
