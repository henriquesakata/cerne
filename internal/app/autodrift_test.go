package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/llm"
	"gitlab.com/rockt/cerne/internal/store"
)

// autoDriftFixture: backend with an implemented F-0001 carrying a result.md.
func autoDriftFixture(t *testing.T) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0001", Title: "Motor", Module: "core", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	docPath := filepath.Join(repoDir, "content", "features", "F-0001", "result.md")
	if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docPath, []byte("# Motor\n\nUsa o parser antigo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repoDir, projDir
}

func TestAutoDriftErrataAppliesWithFake(t *testing.T) {
	repoDir, projDir := autoDriftFixture(t)
	fake := &llm.Fake{Reply: "Errata automática: parser foi trocado."}

	text, err := AutoDriftErrata(context.Background(), repoDir, projDir, "F-0001", "abcdef1234567890", []string{"internal/core/parser.go"}, fake)
	if err != nil {
		t.Fatalf("AutoDriftErrata: %v", err)
	}
	if !strings.Contains(text, "parser foi trocado") {
		t.Fatalf("returned text = %q", text)
	}
	// Prompt was grounded (paths + doc) and asked for errata-only reply.
	if len(fake.Prompts) != 1 || !strings.Contains(fake.Prompts[0], "internal/core/parser.go") || !strings.Contains(fake.Prompts[0], "parser antigo") {
		t.Fatalf("prompt not grounded: %v", fake.Prompts)
	}
	// Errata applied.
	er, err := store.LoadErrata(repoDir, "F-0001")
	if err != nil || !strings.Contains(er, "parser foi trocado") {
		t.Fatalf("errata not applied: %q (err %v)", er, err)
	}
	// History body untouched (crit. 7).
	body, _ := os.ReadFile(filepath.Join(repoDir, "content", "features", "F-0001", "result.md"))
	if string(body) != "# Motor\n\nUsa o parser antigo.\n" {
		t.Fatalf("result.md was mutated:\n%s", body)
	}
	// Events: errata.added with drift-auto actor + drift.auto with payload (crit. 2).
	events, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var sawErrata, sawAuto bool
	for _, ev := range events {
		if ev.EntityID != "F-0001" {
			continue
		}
		switch ev.Type {
		case "errata.added":
			sawErrata = ev.Actor == "drift-auto"
		case "drift.auto":
			sawAuto = ev.Actor == "drift-auto" &&
				strings.HasPrefix(ev.Payload["baseline"], "abcdef1") &&
				strings.Contains(ev.Payload["paths"], "internal/core/parser.go")
		}
	}
	if !sawErrata || !sawAuto {
		t.Fatalf("events missing: errata.added(drift-auto)=%v drift.auto=%v", sawErrata, sawAuto)
	}
}

func TestAutoDriftErrataEmptyReply(t *testing.T) {
	repoDir, projDir := autoDriftFixture(t)
	fake := &llm.Fake{Reply: "   "}

	if _, err := AutoDriftErrata(context.Background(), repoDir, projDir, "F-0001", "abc", []string{"a.go"}, fake); !errors.Is(err, ErrEmptyAutoErrata) {
		t.Fatalf("err = %v, want ErrEmptyAutoErrata", err)
	}
	if er, _ := store.LoadErrata(repoDir, "F-0001"); er != "" {
		t.Fatalf("errata must not be applied on empty reply, got %q", er)
	}
	events, _ := store.ReadEvents(repoDir)
	for _, ev := range events {
		if ev.Type == "drift.auto" {
			t.Fatalf("drift.auto event must not exist on failure")
		}
	}
}

func TestPendingAndAck(t *testing.T) {
	repoDir, projDir := autoDriftFixture(t)
	if err := store.SaveFeature(repoDir, store.Feature{ID: "F-0002", Title: "Outro", Module: "core", State: domain.StateImplemented}); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	doc2 := filepath.Join(repoDir, "content", "features", "F-0002", "result.md")
	if err := os.MkdirAll(filepath.Dir(doc2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc2, []byte("# Outro\n\nCorpo.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &llm.Fake{Reply: "errata"}
	for _, id := range []string{"F-0001", "F-0002"} {
		if _, err := AutoDriftErrata(context.Background(), repoDir, projDir, id, "abc123", []string{"x.go"}, fake); err != nil {
			t.Fatalf("AutoDriftErrata %s: %v", id, err)
		}
	}

	pending, err := PendingDriftReviews(repoDir)
	if err != nil {
		t.Fatalf("PendingDriftReviews: %v", err)
	}
	if len(pending) != 2 || pending[0].Entity != "F-0001" || pending[1].Entity != "F-0002" {
		t.Fatalf("pending = %+v, want F-0001 and F-0002", pending)
	}

	if err := AckDrift(repoDir, projDir, "F-0001", "humano"); err != nil {
		t.Fatalf("AckDrift: %v", err)
	}
	pending, err = PendingDriftReviews(repoDir)
	if err != nil {
		t.Fatalf("PendingDriftReviews after ack: %v", err)
	}
	if len(pending) != 1 || pending[0].Entity != "F-0002" {
		t.Fatalf("pending after ack = %+v, want only F-0002", pending)
	}

	// Ack without pending is the sentinel (crit. 6).
	if err := AckDrift(repoDir, projDir, "F-0001", "humano"); !errors.Is(err, ErrNoPendingDrift) {
		t.Fatalf("second ack err = %v, want ErrNoPendingDrift", err)
	}
}
