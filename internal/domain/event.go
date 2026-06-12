package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Event is a single append-only log entry.
type Event struct {
	ID       string            `json:"id"`
	At       time.Time         `json:"at"`
	EntityID string            `json:"entity_id"`
	Type     string            `json:"type"`
	Payload  map[string]string `json:"payload"`
	Actor    string            `json:"actor"`
}

var (
	// ErrSupersedeByRequired is the sentinel for a supersede payload missing "by".
	ErrSupersedeByRequired = errors.New(`supersede payload requires non-empty "by"`)
	// ErrSupersedeReasonRequired is the sentinel for a supersede payload missing "reason".
	ErrSupersedeReasonRequired = errors.New(`supersede payload requires non-empty "reason"`)
)

// NewEvent builds an Event with a random 16-char hex ID and At = now (UTC).
func NewEvent(entityID, eventType string, payload map[string]string, actor string) Event {
	b := make([]byte, 8)
	rand.Read(b) // never fails per crypto/rand docs
	return Event{
		ID:       hex.EncodeToString(b),
		At:       time.Now().UTC(),
		EntityID: entityID,
		Type:     eventType,
		Payload:  payload,
		Actor:    actor,
	}
}

// MarshalNDJSON encodes the event as a single JSON line without a trailing newline.
func (e Event) MarshalNDJSON() ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalNDJSON parses one NDJSON line into an Event.
func UnmarshalNDJSON(line []byte) (Event, error) {
	var e Event
	if err := json.Unmarshal(line, &e); err != nil {
		return Event{}, err
	}
	return e, nil
}

// ValidateSupersedePayload requires non-empty "by" and "reason" keys.
func ValidateSupersedePayload(payload map[string]string) error {
	if payload["by"] == "" {
		return ErrSupersedeByRequired
	}
	if payload["reason"] == "" {
		return ErrSupersedeReasonRequired
	}
	return nil
}
