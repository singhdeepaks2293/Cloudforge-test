// Package ids generates UUIDv4-formatted identifiers (stdlib only).
// PostgreSQL tables use UUID primary keys, so in-memory stores use the
// same format to keep IDs interchangeable between the two paths.
package ids

import (
	"crypto/rand"
	"fmt"
)

// New returns a random UUIDv4 string.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
