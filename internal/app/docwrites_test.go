package app

import (
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
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
			return RecordPushedDocs(repoDir, projDir, []FileChange{
				{Path: "content/features/F-0003/plan.md", Content: []byte("x")},
				{Path: "content/features/F-0003/notes.md", Content: []byte("not a lifecycle doc")},
				{Path: "content/decisions/D-0001.md", Content: []byte("not a feature")},
			})
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
