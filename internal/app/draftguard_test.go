package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// preambleF0287 is verbatim what `cerne result draft F-0287` wrote into
// content/features/F-0287/result.md (171 bytes): the agent's opening remarks,
// stamped as if they were the delivery record. It is the F-0290 spec's own
// reproduction and the fixture criterion 3 demands.
const preambleF0287 = "Vou levantar a evidência real no worktree antes de escrever o registro.\n\n" +
	"Let me start by examining the actual state of the delivery.\n"

// draftSpec is shaped like a real drafted spec: the metadata lines the template
// asks for come BEFORE the first heading, which is why the draft rule cannot
// copy ValidatePageBody's "first line is a heading".
var draftSpec = "**Tipo:** Corretivo\n**Origem:** Débito do F-0287\n**Nível:** padrão — guarda em um choke point\n\n" +
	"## Problema\n\n" + strings.Repeat("O preâmbulo do agente vira o documento. ", 10)

// TestValidateDraftBody is F-0290 criterion 3: the preamble the spec recorded
// must be refused, and a real draft must pass.
func TestValidateDraftBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"the F-0287 preamble verbatim", preambleF0287, true},
		{"empty", "", true},
		{"whitespace only", "\n\n   \n", true},
		{"a report with no heading at all", "Escrevi o resultado em /tmp/result.md com toda a evidência da entrega, revisada e completa.", true},
		{"a heading but nothing under it", "# F-0290\n\nPronto.\n", true},
		{"a real draft", draftSpec, false},
		{"a real draft the model wrapped in frontmatter", "---\ncerne_id: F-0290\n---\n\n" + draftSpec, false},
		{"a real draft opening with its title", "# F-0290\n\n" + draftSpec, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDraftBody(tc.body)
			if tc.wantErr {
				if !errors.Is(err, ErrNotADocument) {
					t.Fatalf("ValidateDraftBody = %v, want errors.Is ErrNotADocument", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateDraftBody = %v, want nil", err)
			}
		})
	}
}

// TestWriteStampedDraftRefusesPreamble is F-0290 criterion 1: the single write
// point refuses the preamble, names the refused document, and leaves nothing on
// disk — the slot stays empty, as it already does when a draft fails.
func TestWriteStampedDraftRefusesPreamble(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content", "features", "F-0287", "result.md")

	err := WriteStampedDraft(path, "F-0287", "result", preambleF0287)
	if !errors.Is(err, ErrNotADocument) {
		t.Fatalf("WriteStampedDraft = %v, want errors.Is ErrNotADocument", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the refused document: %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("os.Stat(%s) = %v, want ErrNotExist — the refused draft was written", path, statErr)
	}
}

// TestWriteStampedDraftStamps pins the write path: cerne's own cerne_id
// frontmatter followed by the trimmed body, never the model's frontmatter.
func TestWriteStampedDraftStamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content", "features", "F-0290", "spec.md")

	if err := WriteStampedDraft(path, "F-0290", "spec", "\n\n"+draftSpec+"\n\n"); err != nil {
		t.Fatalf("WriteStampedDraft = %v, want nil", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "---\ncerne_id: F-0290\n---\n\n" + strings.TrimSpace(draftSpec) + "\n"
	if string(data) != want {
		t.Errorf("stamped draft =\n%q\nwant\n%q", data, want)
	}
}

// TestWriteStampedDraftKeepsOneFrontmatter (F-0328 critic C8): the draft
// prompt embeds the current document, frontmatter and all, so a model that
// echoes the block gets one block stored, not cerne's stamp over its copy; a
// block naming another document is refused and nothing is written.
func TestWriteStampedDraftKeepsOneFrontmatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "content", "features", "F-0290", "spec.md")
	if err := WriteStampedDraft(path, "F-0290", "spec", "---\ncerne_id: F-0290\n---\n\n"+draftSpec); err != nil {
		t.Fatalf("WriteStampedDraft = %v, want nil", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "---\ncerne_id: F-0290\n---\n\n"+strings.TrimSpace(draftSpec)+"\n" {
		t.Fatalf("echoed frontmatter stored as %q, want one block", data)
	}
	other := filepath.Join(t.TempDir(), "content", "features", "F-0291", "spec.md")
	err := WriteStampedDraft(other, "F-0291", "spec", "---\ncerne_id: F-0007\n---\n\n"+draftSpec)
	if !errors.Is(err, ErrBodyFrontmatter) || !strings.Contains(err.Error(), other) {
		t.Fatalf("WriteStampedDraft = %v, want ErrBodyFrontmatter naming %s", err, other)
	}
	if _, statErr := os.Stat(other); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("os.Stat(%s) = %v, want ErrNotExist — the refused draft was written", other, statErr)
	}
}

// TestDraftOutputContract is F-0290 criterion 2: the contract every draft prompt
// states, in the same shape BuildPagePrompt uses — say what the answer IS, or a
// Claude Code session may answer with a report about having produced it.
func TestDraftOutputContract(t *testing.T) {
	for _, want := range []string{"SOMENTE", "saída padrão", "Não escreva arquivos", "não relate o que você fez"} {
		if !strings.Contains(DraftOutputContract, want) {
			t.Errorf("DraftOutputContract missing %q:\n%s", want, DraftOutputContract)
		}
	}
}
