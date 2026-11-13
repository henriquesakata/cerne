package app

import (
	"os"
	"strings"

	"github.com/henriquesakata/cerne/internal/cost"
	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
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
		if model, found := cost.AuthorModel(cost.ClaudeRoots(), sess, invocation(os.Args)); found {
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
