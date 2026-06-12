package domain

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

func TestEventRoundTrip(t *testing.T) {
	ev := NewEvent("feat-001", "supersede", map[string]string{"by": "feat-002", "reason": "merged into finance"}, "henrique")

	line, err := ev.MarshalNDJSON()
	if err != nil {
		t.Fatalf("MarshalNDJSON: %v", err)
	}

	got, err := UnmarshalNDJSON(line)
	if err != nil {
		t.Fatalf("UnmarshalNDJSON: %v", err)
	}

	if got.ID != ev.ID {
		t.Errorf("ID = %q, want %q", got.ID, ev.ID)
	}
	if !got.At.Equal(ev.At) {
		t.Errorf("At = %v, want %v", got.At, ev.At)
	}
	if got.EntityID != ev.EntityID {
		t.Errorf("EntityID = %q, want %q", got.EntityID, ev.EntityID)
	}
	if got.Type != ev.Type {
		t.Errorf("Type = %q, want %q", got.Type, ev.Type)
	}
	if got.Actor != ev.Actor {
		t.Errorf("Actor = %q, want %q", got.Actor, ev.Actor)
	}
	if len(got.Payload) != len(ev.Payload) {
		t.Fatalf("Payload has %d keys, want %d", len(got.Payload), len(ev.Payload))
	}
	for k, want := range ev.Payload {
		if got.Payload[k] != want {
			t.Errorf("Payload[%q] = %q, want %q", k, got.Payload[k], want)
		}
	}
}

func TestNewEventID(t *testing.T) {
	a := NewEvent("e", "create", nil, "x")
	b := NewEvent("e", "create", nil, "x")

	if len(a.ID) != 16 {
		t.Fatalf("ID length = %d, want 16", len(a.ID))
	}
	if _, err := hex.DecodeString(a.ID); err != nil {
		t.Fatalf("ID %q is not hex: %v", a.ID, err)
	}
	if a.ID == b.ID {
		t.Fatalf("two events share ID %q", a.ID)
	}
	if a.At.IsZero() {
		t.Fatal("At is zero, want time.Now().UTC()")
	}
	if a.At.Location() != nil && a.At.Location().String() != "UTC" {
		t.Fatalf("At location = %v, want UTC", a.At.Location())
	}
}

func TestMarshalNDJSONSingleLine(t *testing.T) {
	ev := NewEvent("feat-001", "create", map[string]string{"note": "line1\nline2"}, "henrique")

	line, err := ev.MarshalNDJSON()
	if err != nil {
		t.Fatalf("MarshalNDJSON: %v", err)
	}
	if bytes.ContainsRune(line, '\n') {
		t.Fatalf("output contains newline: %q", line)
	}
}

func TestMarshalNDJSONFieldNames(t *testing.T) {
	ev := NewEvent("feat-001", "create", map[string]string{"k": "v"}, "henrique")

	line, err := ev.MarshalNDJSON()
	if err != nil {
		t.Fatalf("MarshalNDJSON: %v", err)
	}
	for _, key := range []string{`"id":`, `"at":`, `"entity_id":`, `"type":`, `"payload":`, `"actor":`} {
		if !bytes.Contains(line, []byte(key)) {
			t.Errorf("output missing %s: %s", key, line)
		}
	}
}

func TestValidateSupersedePayload(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]string
		wantErr error
	}{
		{"valid", map[string]string{"by": "feat-002", "reason": "merged"}, nil},
		{"missing by", map[string]string{"reason": "merged"}, ErrSupersedeByRequired},
		{"empty by", map[string]string{"by": "", "reason": "merged"}, ErrSupersedeByRequired},
		{"missing reason", map[string]string{"by": "feat-002"}, ErrSupersedeReasonRequired},
		{"empty reason", map[string]string{"by": "feat-002", "reason": ""}, ErrSupersedeReasonRequired},
		{"nil payload", nil, ErrSupersedeByRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSupersedePayload(c.payload)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want errors.Is %v", err, c.wantErr)
			}
		})
	}
}
