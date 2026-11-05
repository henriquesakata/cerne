package models

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// handEdited is a global file as a person writes it: an unknown top-level
// key, an entry that switches efforts off explicitly ("efforts": [] — not the
// same as leaving the field out, which inherits the baseline) and an entry
// with a field cerne does not know.
const handEdited = `{
  "note": "kept by hand",
  "default_model": "claude-sonnet-5-5",
  "entries": [
    {"id": "claude-opus-5-5", "efforts": [], "note": "no effort for opus"},
    {"id": "claude-sonnet-5", "visibility": "current", "x-extra": {"a": 1}}
  ]
}`

func decode(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s is not JSON: %v", path, err)
	}
	return v
}

// TestHandEditsSurviveProgramWrites: C8 — a provider PUT and a refresh write
// only the keys they own; everything else in a hand-edited file (unknown
// fields, an explicit "efforts": [], untouched entries) reads back the same.
func TestHandEditsSurviveProgramWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(handEdited), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := decode(t, path)
	opus, _ := Load(dir)
	if e, _ := opus.Find("claude-opus-5-5"); e.Efforts == nil || len(e.Efforts) != 0 {
		t.Fatalf("explicit empty efforts read as %#v, want an empty (non-inherited) set", e.Efforts)
	}

	if err := SetPreferredProvider(dir, "anthropic"); err != nil {
		t.Fatalf("SetPreferredProvider: %v", err)
	}
	got := decode(t, path)
	if got["preferred_provider"] != "anthropic" {
		t.Fatalf("preferred_provider = %v", got["preferred_provider"])
	}
	delete(got, "preferred_provider")
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("provider PUT rewrote untouched keys:\n got %v\nwant %v", got, orig)
	}

	listed := func(context.Context) ([]Entry, error) {
		return append(listedAll("anthropic"), Entry{ID: "claude-new-9", Label: "Claude New 9"}), nil
	}
	if _, err := Refresh(context.Background(), dir, "anthropic", listed, now); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	got = decode(t, path)
	entries := got["entries"].([]any)
	if len(entries) != 3 || entries[2].(map[string]any)["id"] != "claude-new-9" {
		t.Fatalf("entries after refresh = %v, want the two hand entries then claude-new-9", entries)
	}
	got["entries"] = entries[:2]
	delete(got, "preferred_provider")
	delete(got, "updated_at")
	if !reflect.DeepEqual(got, orig) {
		t.Fatalf("refresh rewrote untouched keys:\n got %v\nwant %v", got, orig)
	}
	after, _ := Load(dir)
	if e, _ := after.Find("claude-opus-5-5"); len(e.Efforts) != 0 {
		t.Fatalf("explicit empty efforts lost: %v", e.Efforts)
	}
}
