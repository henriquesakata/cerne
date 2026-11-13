package app

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/domain"
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
		a := LLMAuthor(cfg.LLM)
		if m := strings.TrimSpace(os.Getenv("CERNE_MODEL")); m != "" {
			a.Model = m
		}
		return a
	}
	if !isAgentActor(EventActor()) {
		return HumanAuthor()
	}
	if sess := os.Getenv("CERNE_SESSION"); sess != "" {
		if model, found := cost.AuthorModel(CostRoots(), sess, invocation(os.Args)); found {
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

// RecordDocWrite appends the feature.doc_edited event of a write of feature
// id's doc (spec|plan|result) by a, inside the caller's critical section: the
// record of a write that does not go through SaveFeatureDoc (a draft).
func RecordDocWrite(repoDir, id, doc string, a DocAuthor) error {
	f, err := store.LoadFeature(repoDir, id)
	if err != nil {
		return err
	}
	return store.AppendEvent(repoDir, a.docEdited(id, doc, f.State, nil))
}

// RecordPushedDocs appends, inside the push's critical section (the mutate of
// ReconcileCommitPushMutate), one feature.doc_edited per feature lifecycle
// document among changes, by the process's author — with no rev, so it never
// reads as an amend. A document equal to the tree the mutate runs on, or of a
// feature the backend does not have, records nothing.
func RecordPushedDocs(repoDir, projDir string, changes []FileChange) error {
	var a *DocAuthor
	for _, ch := range changes {
		rest, ok := strings.CutPrefix(ch.Path, "content/features/")
		id, file, _ := strings.Cut(rest, "/")
		doc := strings.TrimSuffix(file, ".md")
		if !ok || ch.Deleted || featureDocFilenames[doc] != file {
			continue
		}
		if cur, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(ch.Path))); err == nil && bytes.Equal(cur, ch.Content) {
			continue
		}
		f, err := store.LoadFeature(repoDir, id)
		if err != nil {
			continue
		}
		if a == nil {
			v := docAuthor(projDir)
			a = &v
		}
		if err := store.AppendEvent(repoDir, a.docEdited(id, doc, f.State, nil)); err != nil {
			return err
		}
	}
	return nil
}

// invocation is the command words this process was called with: the leading
// arguments before the first flag, at most two ("push", "feature spec").
func invocation(args []string) []string {
	var words []string
	for _, a := range args[min(1, len(args)):] {
		if strings.HasPrefix(a, "-") || len(words) == 2 {
			break
		}
		words = append(words, a)
	}
	return words
}
