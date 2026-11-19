package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ErrContentConflict is a write whose base — the hash of the content the
// writer loaded — no longer matches the backend's (F-0464): someone else
// changed the document since, and the write is refused rather than lost.
var ErrContentConflict = errors.New("the document changed in the backend since it was loaded")

// ContentConflictError is ErrContentConflict carrying the content the core
// compared against, so the caller can show the difference without a re-read.
type ContentConflictError struct{ Current string }

func (e *ContentConflictError) Error() string { return ErrContentConflict.Error() }

// Is matches ErrContentConflict.
func (e *ContentConflictError) Is(target error) bool { return target == ErrContentConflict }

// ContentHash is the sha256 of the exact stored bytes, lowercase hex — the
// value a read returns as content_hash and a write sends back as If-Match.
// A missing file counts as empty bytes.
func ContentHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// checkBase refuses cur when a base was given and cur's hash is not it.
func checkBase(cur []byte, base string) error {
	if base != "" && ContentHash(cur) != base {
		return &ContentConflictError{Current: string(cur)}
	}
	return nil
}
