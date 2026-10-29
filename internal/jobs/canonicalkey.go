package jobs

import (
	"strings"
)

// A job's canonical key is "type:storefront:adamID". It is the dedup identity
// enforced by the partial unique index on jobs(canonical_key) over the
// queued/running statuses, and it is also the record of what was parsed at
// submission time, replayed by the download worker instead of re-parsing the
// raw input.
//
// Rows written while submissions could pick their own downloads directory
// carry a fourth segment, a hash of that directory. ParseCanonicalKey still
// reads those keys; such a row only misses dedup against a job submitted
// since, which can cost one extra download, never a wrong one.
//
// Build and parse live together in this file on purpose. They are the two
// halves of one format, they sit in different packages at their call sites
// (internal/jobs writes the key, internal/media reads it back), and a change
// to one without the other is silent: an extra segment would simply shift
// which substring the worker believes is the adam id, and it would download
// the wrong thing without erroring.

// legacyDestinationSegmentLen is the width, in hex digits, of the destination
// segment older keys end with.
const legacyDestinationSegmentLen = 12

// canonicalKey joins the three segments, verbatim and human-readable.
func canonicalKey(jobType, storefront, id string) string {
	return jobType + ":" + storefront + ":" + id
}

// ParseCanonicalKey recovers the submission-time parse result from a stored
// key. It reports false for a key it cannot make sense of, leaving the caller
// to fall back to re-parsing the raw input.
//
// It accepts the four-segment keys of older rows, so no data migration is
// needed: those rows keep resolving to the same type/storefront/adam id.
func ParseCanonicalKey(key string) (jobType, storefront, id string, ok bool) {
	parts := strings.Split(key, ":")
	if len(parts) < 3 {
		return "", "", "", false
	}
	idParts := parts[2:]
	// Drop an older key's trailing destination segment, but only when
	// something is left for the id. The length guard is what keeps a
	// three-segment key whose adam id happens to be 12 hex-looking digits
	// from being read as a destination with an empty id.
	if len(idParts) > 1 && isLegacyDestinationSegment(idParts[len(idParts)-1]) {
		idParts = idParts[:len(idParts)-1]
	}
	// Re-join instead of taking idParts[0]. An adam id is the last path
	// segment of a submitted URL, ':' is legal there and nothing rejects it,
	// so an id containing one has to survive the round trip intact — this is
	// the case a plain fixed-count split would silently truncate.
	id = strings.Join(idParts, ":")
	jobType, storefront = parts[0], parts[1]
	if jobType == "" || storefront == "" || id == "" {
		return "", "", "", false
	}
	return jobType, storefront, id, true
}

func isLegacyDestinationSegment(s string) bool {
	if len(s) != legacyDestinationSegmentLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
