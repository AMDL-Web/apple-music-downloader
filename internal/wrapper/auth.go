package wrapper

import (
	"io"
	"net/http"
	"strings"
)

// pairedTransport sends the pairing's access token on every request and,
// after a 401, renews it once and sends the request again.
type pairedTransport struct {
	pairing *Pairing
	next    http.RoundTripper
}

func (t pairedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token := t.pairing.accessToken(req.Context())
	if token == "" {
		resp, err := t.next.RoundTrip(req)
		if err == nil && resp.StatusCode == http.StatusUnauthorized {
			t.pairing.askedForToken()
		}
		return resp, err
	}
	resp, err := t.next.RoundTrip(withBearer(req, token))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	retry := t.pairing.unauthorized(req.Context(), token)
	if retry == "" || (req.Body != nil && req.Body != http.NoBody && req.GetBody == nil) {
		return resp, nil
	}
	again := withBearer(req, retry)
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return resp, nil
		}
		again.Body = body
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	resp, err = t.next.RoundTrip(again)
	if err == nil && resp.StatusCode == http.StatusUnauthorized {
		// A renewed token refused too: the pairing itself is gone.
		t.pairing.drop("the wrapper refused a renewed access token; pair again")
	}
	return resp, err
}

func withBearer(req *http.Request, token string) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	return out
}
