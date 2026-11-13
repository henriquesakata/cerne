package app

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// lastDocEdit returns the newest feature.doc_edited event of id.
func lastDocEdit(t *testing.T, repoDir, id string) (domain.Event, int) {
	t.Helper()
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	var last domain.Event
	n := 0
	for _, ev := range evs {
		if ev.EntityID == id && ev.Type == "feature.doc_edited" {
			last, n = ev, n+1
		}
	}
	return last, n
}

// TestWritePointsRecordAuthor (F-0375 crit. 1): every point that writes a
// feature document records, on its feature.doc_edited event, the document, the
// provider, the model and the source — the CLI ones from the environment (here
// declared), the UI editor and clarifications as a human, the chat and the
// drafts as the [llm] config they called; amends keep reason and rev.
func TestWritePointsRecordAuthor(t *testing.T) {
	authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-x")
	declared := DocAuthor{Provider: "openai", Model: "gpt-x", Source: "declared"}
	chat := LLMAuthor(project.LLMConfig{Mode: "byo-key", Model: "claude-sonnet-5"})
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateImplemented, "spec.md", "result.md")
	spec := func(s string) string { return "# Spec\n\n## Problema\n\n" + s + "\n" + testBarTail }

	cases := []struct {
		name, id, doc string
		write         func() error
		want          DocAuthor
		extra         map[string]string
	}{
		{"feature new --file", "F-0003", "spec", func() error {
			_, err := CreateFeatureWithSpec(repoDir, projDir, "Nova", "", "", nil, nil, spec("nova"), false, "", "")
			return err
		}, declared, nil},
		{"feature spec", "F-0003", "spec", func() error {
			_, _, err := UpdateCandidateSpec(repoDir, projDir, "F-0003", spec("revista"))
			return err
		}, declared, nil},
		{"chat confirmation, update", "F-0003", "spec", func() error {
			_, _, err := UpdateCandidateSpecBy(repoDir, projDir, "F-0003", spec("do chat"), chat)
			return err
		}, chat, nil},
		{"chat confirmation, create", "F-0004", "spec", func() error {
			_, err := CreateFeatureWithSpecBy(repoDir, projDir, "Do chat", "", "", nil, nil, spec("chat"), chat)
			return err
		}, chat, nil},
		{"UI editor", "F-0003", "spec", func() error {
			return SaveFeatureDocBy(repoDir, projDir, "F-0003", "spec", "---\ncerne_id: F-0003\n---\n\n"+spec("editor"), HumanAuthor())
		}, HumanAuthor(), nil},
		{"SaveFeatureDoc outside the UI", "F-0003", "plan", func() error {
			return SaveFeatureDoc(repoDir, projDir, "F-0003", "plan", "# Plano\n\n## Tarefas\n\n1. Fazer.\n")
		}, declared, nil},
		{"feature amend", "F-0001", "spec", func() error {
			_, _, _, err := AmendFeatureDoc(repoDir, projDir, "F-0001", "spec", spec("emendada"), "mudou")
			return err
		}, declared, map[string]string{"reason": "mudou", "rev": "1"}},
		{"result amend", "F-0002", "result", func() error {
			_, _, err := AmendFeatureResult(repoDir, projDir, "F-0002", amendedBody, "faltou prova")
			return err
		}, declared, map[string]string{"reason": "faltou prova"}},
		{"push", "F-0003", "plan", func() error {
			changes := []FileChange{
				{Path: "content/features/F-0003/plan.md", Content: []byte("x")},
				{Path: "content/features/F-0003/notes.md", Content: []byte("not a lifecycle doc")},
				{Path: "content/decisions/D-0001.md", Content: []byte("not a feature")},
			}
			return RecordPushedDocs(repoDir, changes, PushAuthor(projDir, changes))
		}, declared, nil},
		{"draft", "F-0003", "result", func() error {
			return RecordDocWrite(repoDir, "F-0003", "result", chat)
		}, chat, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.write(); err != nil {
				t.Fatalf("write: %v", err)
			}
			ev, _ := lastDocEdit(t, repoDir, tc.id)
			f, _ := store.LoadFeature(repoDir, tc.id)
			want := map[string]string{"doc": tc.doc, "state": string(f.State),
				"provider": tc.want.Provider, "model": tc.want.Model, "source": tc.want.Source}
			for k, v := range tc.extra {
				want[k] = v
			}
			if len(ev.Payload) != len(want) {
				t.Fatalf("payload = %v, want %v", ev.Payload, want)
			}
			for k, v := range want {
				if ev.Payload[k] != v {
					t.Fatalf("payload[%s] = %q, want %q (payload %v)", k, ev.Payload[k], v, ev.Payload)
				}
			}
		})
	}
	// The push event carries no rev: show's amendments list stays the amends'.
	if ev, _ := lastDocEdit(t, repoDir, "F-0003"); ev.Payload["rev"] != "" {
		t.Fatalf("a non-amend write carries rev %q", ev.Payload["rev"])
	}
}

// TestStubWritesRecordNothing (F-0375 crit. 1, 11): a feature born without a
// spec body gets cerne's stub, which nobody wrote — no doc_edited event.
func TestStubWritesRecordNothing(t *testing.T) {
	authorEnv(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Sem spec", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, n := lastDocEdit(t, repoDir, "F-0001"); n != 0 {
		t.Fatalf("doc_edited events for a stub = %d, want 0", n)
	}
}

// TestClarificationAuthors (F-0375 crit. 7): an answer from the page (the At
// form) is a human's; the CLI's numbered form is whoever runs it.
func TestClarificationAuthors(t *testing.T) {
	authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_PROVIDER", "google")
	repoDir, projDir := newSnapshotBackend(t, false)
	seedClarifyFixture(t, repoDir)
	items, err := ListClarifications(repoDir, "F-0001")
	if err != nil || len(items) < 2 {
		t.Fatalf("ListClarifications = %v, %v", items, err)
	}
	if _, err := AnswerClarificationAt(repoDir, projDir, "F-0001", items[0].Doc, items[0].Offset, items[0].Raw, "sincrona"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := lastDocEdit(t, repoDir, "F-0001"); ev.Payload["source"] != "human" {
		t.Fatalf("page answer source = %q, want human", ev.Payload["source"])
	}
	if _, err := AnswerClarification(repoDir, projDir, "F-0001", 1, "", "x"); err != nil {
		t.Fatal(err)
	}
	if ev, _ := lastDocEdit(t, repoDir, "F-0001"); ev.Payload["source"] != "declared" || ev.Payload["provider"] != "google" {
		t.Fatalf("CLI answer = %v, want declared google", ev.Payload)
	}
}

// TestAuthorRecordLeavesBodiesAndGatesAlone (F-0375 crit. 10, 12): recording
// the writer never touches a document — every body stored is byte for byte
// the one written, the approved spec stays approved, nothing names a model in
// any document — and lint finds exactly what it found before the records.
func TestAuthorRecordLeavesBodiesAndGatesAlone(t *testing.T) {
	authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-x")
	repoDir, projDir, _ := newFeatureBackend(t)
	spec := "# Spec\n\n## Problema\n\nCorpo real.\n" + testBarTail
	if _, err := CreateFeatureWithSpec(repoDir, projDir, "Corpo intacto", "", "", nil, nil, spec, false, "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	specBefore, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md"))
	f, _ := store.LoadFeature(repoDir, "F-0001")
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale {
		t.Fatal("fresh approval reads stale")
	}
	plan := "# Plano\n\n## Tarefas\n\n1. Fazer.\n"
	if err := SaveFeatureDoc(repoDir, projDir, "F-0001", "plan", plan); err != nil {
		t.Fatal(err)
	}
	// The records below are events only: lint must not see them.
	findingsBefore, err := lint.Run(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordDocWrite(repoDir, "F-0001", "spec", DocAuthor{Provider: "anthropic", Model: "claude-opus-5-5", Source: "transcript"}); err != nil {
		t.Fatal(err)
	}
	pushed := []FileChange{{Path: "content/features/F-0001/plan.md", Content: []byte(plan + "\nmais\n")}}
	if err := RecordPushedDocs(repoDir, pushed, PushAuthor(projDir, pushed)); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "spec.md")); string(got) != string(specBefore) {
		t.Fatalf("spec.md changed:\n%q\nwant\n%q", got, specBefore)
	}
	if got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", "plan.md")); string(got) != plan {
		t.Fatalf("plan.md = %q, want exactly the written body %q", got, plan)
	}
	for _, doc := range []string{"spec.md", "plan.md"} {
		got, _ := os.ReadFile(FeatureDocPath(repoDir, "F-0001", doc))
		if strings.Contains(string(got), "gpt-x") || strings.Contains(string(got), "claude-opus") {
			t.Fatalf("%s names a model:\n%s", doc, got)
		}
	}
	f, _ = store.LoadFeature(repoDir, "F-0001")
	if stale, _, _ := lint.SpecChangedAfterApproval(repoDir, f); stale || f.State != domain.StateApproved {
		t.Fatalf("after the records: stale %v, state %s — want the approval intact", stale, f.State)
	}
	findingsAfter, err := lint.Run(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(findingsAfter) != fmt.Sprint(findingsBefore) {
		t.Fatalf("lint changed with the records:\nbefore %v\nafter  %v", findingsBefore, findingsAfter)
	}
}

// TestPushAuthor (critic round 1, item 3): the push resolves its writer
// before the backend lock, and only when it sends a feature lifecycle
// document — a push of other documents reads no transcript and records
// nothing.
func TestPushAuthor(t *testing.T) {
	authorEnv(t)
	t.Setenv("CERNE_ACTOR", "agent")
	t.Setenv("CERNE_PROVIDER", "openai")
	others := []FileChange{
		{Path: "content/decisions/D-0001.md", Content: []byte("x")},
		{Path: "content/features/F-0001/notes.md", Content: []byte("x")},
		{Path: "content/features/F-0001/spec.md", Deleted: true},
	}
	if got := PushAuthor(t.TempDir(), others); got != (DocAuthor{}) {
		t.Fatalf("PushAuthor(no feature doc) = %+v, want none", got)
	}
	docs := append(others, FileChange{Path: "content/features/F-0001/plan.md", Content: []byte("x")})
	if got := PushAuthor(t.TempDir(), docs); got != (DocAuthor{Provider: "openai", Source: "declared"}) {
		t.Fatalf("PushAuthor(plan) = %+v, want declared openai", got)
	}
	repoDir, _, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	if err := RecordPushedDocs(repoDir, docs, DocAuthor{}); err != nil {
		t.Fatal(err)
	}
	if _, n := lastDocEdit(t, repoDir, "F-0001"); n != 0 {
		t.Fatalf("no writer: %d doc_edited recorded, want 0", n)
	}
}
