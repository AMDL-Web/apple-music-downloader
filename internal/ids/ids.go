// Package ids generates the random identifiers of persisted records: jobs
// ("job_<32 hex>") and items ("item_<32 hex>").
package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// NewID returns prefix, an underscore and 32 lowercase hex digits (128 random
// bits from crypto/rand). The prefix names the record kind, for example "job"
// or "item"; clients treat the whole value as opaque.
func NewID(prefix string) string {
	var b [16]byte
	// crypto/rand.Read never returns an error and never short-reads.
	_, _ = rand.Read(b[:])
	return prefix + "_" + hex.EncodeToString(b[:])
}
