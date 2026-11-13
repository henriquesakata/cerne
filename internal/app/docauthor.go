package app

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/doccheck"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// DocAuthor is who wrote a feature document (F-0375): the provider and model,
// and the source of the value — transcript, declared, llm-config, human or
// unknown. Model is empty for a human and for unknown; it rides on the
// feature.doc_edited payload, never in the document body.
type DocAuthor struct {
	Provider string
	Model    string
	Source   string
}

// transcriptProvider is the provider of a model read from a Claude Code
// transcript: Claude Code speaks to Anthropic's API.
const transcriptProvider = "anthropic"

// HumanAuthor is a human writing a document (the UI editor, a CLI without an
// agent CERNE_ACTOR): no provider, no model.
func HumanAuthor() DocAuthor { return DocAuthor{Source: "human"} }

// LLMAuthor is the [llm] config c of a call cerne itself made (a draft, the
// Backlog chat). claude-cli is never passed --model, so it names no model.
func LLMAuthor(c project.LLMConfig) DocAuthor {
	if llm.ResolveMode(c.Mode) == llm.ModeClaudeCLI {
		return DocAuthor{Provider: llm.ModeClaudeCLI, Source: "llm-config"}
	}
	c = c.WithDefaults()
	return DocAuthor{Provider: c.Provider, Model: c.Model, Source: "llm-config"}
}

// docAuthor resolves who is writing from this process's environment, in the
// order of F-0375's criteria: a chat tool call (CERNE_ORIGIN=chat) is the
// project's [llm] with the call's CERNE_MODEL; no agent CERNE_ACTOR is a
// human; CERNE_SESSION with a readable Claude Code transcript gives its model
// (ambiguous: unknown); then CERNE_PROVIDER/CERNE_MODEL as declared; else
// unknown — never inferred.
func docAuthor(projDir string) DocAuthor {
	unknown := DocAuthor{Source: "unknown"}
	if os.Getenv("CERNE_ORIGIN") == "chat" {
		cfg, _, err := project.LoadConfig(projDir)
		if err != nil {
			return unknown
		}
		// The call's model replaces the config's, as chatAuthor does; LLMAuthor
		// still drops it for claude-cli, which is never passed --model.
		if m := strings.TrimSpace(os.Getenv("CERNE_MODEL")); m != "" {
			cfg.LLM.Model = m
		}
		return LLMAuthor(cfg.LLM)
	}
	if !isAgentActor(EventActor()) {
		return HumanAuthor()
	}
	if sess := os.Getenv("CERNE_SESSION"); sess != "" {
		if model, found := cost.AuthorModel(CostRoots(), sess, os.Args[min(1, len(os.Args)):]); found {
			if model == "" {
				return unknown
			}
			return DocAuthor{Provider: transcriptProvider, Model: model, Source: "transcript"}
		}
	}
	p, m := strings.TrimSpace(os.Getenv("CERNE_PROVIDER")), strings.TrimSpace(os.Getenv("CERNE_MODEL"))
	if p != "" || m != "" {
		return DocAuthor{Provider: p, Model: m, Source: "declared"}
	}
	return unknown
}

// docEdited is the feature.doc_edited event of a write of doc by a: the
// document, the feature's state at write time, the author, plus extra (an
// amend's reason and rev).
func (a DocAuthor) docEdited(id, doc string, state domain.FeatureState, extra map[string]string) domain.Event {
	p := map[string]string{"doc": doc, "state": string(state), "provider": a.Provider, "model": a.Model, "source": a.Source}
	maps.Copy(p, extra)
	return domain.NewEvent(id, "feature.doc_edited", p, EventActor())
}

// planSpecDigest is what a write of feature id's doc adds to its
// feature.doc_edited payload (F-0377): for a plan (body) with real content,
// spec_digest — lint.SpecDigest of the spec it was written against: spec when
// the same write carries one, else the backend's. No spec, no field; nil for
// any other doc or a stub plan.
func planSpecDigest(repoDir, id, doc string, body, spec []byte) map[string]string {
	if doc != "plan" || !doccheck.HasRealContent(body) {
		return nil
	}
	if spec == nil {
		var err error
		if spec, err = os.ReadFile(FeatureDocPath(repoDir, id, "spec.md")); err != nil {
			return nil
		}
	}
	return map[string]string{"spec_digest": lint.SpecDigest(spec)}
}

// RecordDocWrite appends the feature.doc_edited event of a write of feature
// id's doc (spec|plan|result) by a, inside the caller's critical section: the
// record of a write that does not go through SaveFeatureDoc (a draft).
func RecordDocWrite(repoDir, id, doc string, a DocAuthor) error {
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return err
	}
	body, _ := os.ReadFile(FeatureDocPath(repoDir, id, featureDocFilenames[doc]))
	return store.AppendEvent(repoDir, a.docEdited(id, doc, f.State, planSpecDigest(repoDir, id, doc, body, nil)))
}

// pushedFeatureDoc maps a pushed path to its feature and lifecycle document
// (spec|plan|result); ok is false for any other path.
func pushedFeatureDoc(ch FileChange) (id, doc string, ok bool) {
	rest, isFeature := strings.CutPrefix(ch.Path, "content/features/")
	id, file, _ := strings.Cut(rest, "/")
	doc = strings.TrimSuffix(file, ".md")
	return id, doc, isFeature && !ch.Deleted && featureDocFilenames[doc] == file
}

// PushAuthor is who writes a push's feature documents, resolved before the
// push takes the backend lock — the transcript read stays outside it. The
// zero DocAuthor when changes hold no feature lifecycle document: nothing
// to record, nothing read.
func PushAuthor(projDir string, changes []FileChange) DocAuthor {
	for _, ch := range changes {
		if _, _, ok := pushedFeatureDoc(ch); ok {
			return docAuthor(projDir)
		}
	}
	return DocAuthor{}
}

// RecordPushedDocs appends, inside the push's critical section (the mutate of
// ReconcileCommitPushMutate), one feature.doc_edited per feature lifecycle
// document among changes, by by (PushAuthor) — with no rev, so it never reads
// as an amend. A zero by records nothing; so does a document equal to the
// tree the mutate runs on, or of a feature the backend does not have.
func RecordPushedDocs(repoDir string, changes []FileChange, by DocAuthor) error {
	if by == (DocAuthor{}) {
		return nil
	}
	for _, ch := range changes {
		id, doc, ok := pushedFeatureDoc(ch)
		if !ok {
			continue
		}
		if cur, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(ch.Path))); err == nil && bytes.Equal(cur, ch.Content) {
			continue
		}
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			continue
		}
		if err := store.AppendEvent(repoDir, by.docEdited(id, doc, f.State, planSpecDigest(repoDir, id, doc, ch.Content, pushedSpec(changes, id)))); err != nil {
			return err
		}
	}
	return nil
}

// pushedSpec is the spec of feature id among a push's changes, nil when the
// push does not carry it.
func pushedSpec(changes []FileChange, id string) []byte {
	for _, ch := range changes {
		if fid, doc, ok := pushedFeatureDoc(ch); ok && fid == id && doc == "spec" {
			return ch.Content
		}
	}
	return nil
}

// DocWrite is one recorded write of a feature document (F-0375).
type DocWrite struct {
	Doc      string    `json:"doc"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Source   string    `json:"source"`
	At       time.Time `json:"at"`
}

// Who is how a write names its writer: provider/model, the provider alone,
// or the source (human, unknown) when neither is known.
func (w DocWrite) Who() string {
	switch {
	case w.Model != "" && w.Provider != "":
		return w.Provider + "/" + w.Model
	case w.Model != "":
		return w.Model
	case w.Provider != "":
		return w.Provider
	}
	return w.Source
}

// DocWrites lists the recorded writes of feature id's documents, oldest
// first, at in UTC (every surface prints the same date): its
// feature.doc_edited events that carry a source. One from before F-0375
// carries none and is left out — no writer is invented for it.
func DocWrites(repoDir, id string) ([]DocWrite, error) {
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		return nil, err
	}
	var out []DocWrite
	for _, ev := range evs {
		if p := ev.Payload; ev.EntityID == id && ev.Type == "feature.doc_edited" && p["source"] != "" {
			out = append(out, DocWrite{p["doc"], p["provider"], p["model"], p["source"], ev.At.UTC()})
		}
	}
	slices.SortStableFunc(out, func(a, b DocWrite) int { return a.At.Compare(b.At) })
	return out, nil
}
