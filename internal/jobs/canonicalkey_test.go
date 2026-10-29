package jobs

import (
	"context"
	"testing"
)

// TestSubmitBatchStoresThreeSegmentKey pins the key a submission writes: the
// dedup identity is what is downloaded, type, storefront and adam id.
func TestSubmitBatchStoresThreeSegmentKey(t *testing.T) {
	manager := newTestManager(t)
	resp := manager.SubmitBatch(context.Background(), []string{"album|us|111"}, nil)
	if resp.Accepted != 1 || resp.Results[0].Job == nil {
		t.Fatalf("submit = %+v, want one accepted job", resp)
	}
	job, err := manager.store.GetJob(context.Background(), resp.Results[0].Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.CanonicalKey != "album:us:111" {
		t.Fatalf("canonical key = %q, want album:us:111", job.CanonicalKey)
	}
}

func TestParseCanonicalKey(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		key                         string
		wantOK                      bool
		wantType, wantStore, wantID string
	}{
		{name: "built key", key: canonicalKey("album", "us", "111"), wantOK: true, wantType: "album", wantStore: "us", wantID: "111"},
		{name: "station id", key: "station:us:ra.985484166", wantOK: true, wantType: "station", wantStore: "us", wantID: "ra.985484166"},
		{name: "playlist id", key: "playlist:us:pl.u-abcdef", wantOK: true, wantType: "playlist", wantStore: "us", wantID: "pl.u-abcdef"},
		// Older rows end with a 12-hex-digit destination segment and must
		// resolve unchanged, which is what makes this a no-migration change.
		{name: "legacy destination segment", key: "album:us:111:0123456789ab", wantOK: true, wantType: "album", wantStore: "us", wantID: "111"},
		{name: "legacy id containing a colon", key: "song:us:od:d:0123456789ab", wantOK: true, wantType: "song", wantStore: "us", wantID: "od:d"},
		// A twelve-digit adam id is valid hex. Without the "something must be
		// left for the id" guard this would be mistaken for a destination.
		{name: "hex-shaped id", key: "song:us:123456789012", wantOK: true, wantType: "song", wantStore: "us", wantID: "123456789012"},
		// ':' is legal in a URL path segment and nothing rejects it, so an id
		// containing one must survive both directions intact.
		{name: "id containing a colon", key: canonicalKey("song", "us", "od:d"), wantOK: true, wantType: "song", wantStore: "us", wantID: "od:d"},
		{name: "too few segments", key: "song:us", wantOK: false},
		{name: "empty", key: "", wantOK: false},
		{name: "empty storefront", key: "song::111", wantOK: false},
		{name: "empty id", key: "song:us:", wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobType, storefront, id, ok := ParseCanonicalKey(tc.key)
			if ok != tc.wantOK {
				t.Fatalf("ParseCanonicalKey(%q) ok = %v, want %v", tc.key, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if jobType != tc.wantType || storefront != tc.wantStore || id != tc.wantID {
				t.Fatalf("ParseCanonicalKey(%q) = %q/%q/%q, want %q/%q/%q",
					tc.key, jobType, storefront, id, tc.wantType, tc.wantStore, tc.wantID)
			}
		})
	}
}
