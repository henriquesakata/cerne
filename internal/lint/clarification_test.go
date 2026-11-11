package lint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestClarificationFindings pins the per-doc rule: every open [ESCLARECER]
// marker yields one finding carrying the question; resolved [ESCLARECIDO]
// markers and marker-free prose yield nothing.
func TestClarificationFindings(t *testing.T) {
	d := scannedDoc{entity: "F-0001", rel: "content/features/F-0001/spec.md"}

	tests := []struct {
		name string
		text string
		want []string // expected Detail substrings, in order
	}{
		{
			name: "clean doc",
			text: "prosa sem marcador",
			want: nil,
		},
		{
			name: "one open question",
			text: "x [ESCLARECER: qual formato usar?] y",
			want: []string{"qual formato usar?"},
		},
		{
			name: "bare marker",
			text: "x [ESCLARECER] y",
			want: []string{"no question"},
		},
		{
			name: "resolved marker is silent",
			text: "[ESCLARECIDO: qual formato? → pipe inline]",
			want: nil,
		},
		{
			name: "two open one resolved",
			text: "[ESCLARECER: um?] meio [ESCLARECIDO: velho? → ok] fim [ESCLARECER: dois?]",
			want: []string{"um?", "dois?"},
		},
		// F-0182: the en vocabulary gates identically — before it entered
		// the catalog, an English doc's open questions were invisible here.
		{
			name: "en open question",
			text: "x [CLARIFY: which storage backend?] y",
			want: []string{"which storage backend?"},
		},
		{
			name: "en resolved marker is silent",
			text: "[CLARIFIED: which backend? → S3]",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clarificationFindings(d, tt.text)
			if len(got) != len(tt.want) {
				t.Fatalf("clarificationFindings() = %d findings, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, f := range got {
				if f.Rule != RuleOpenClarification {
					t.Errorf("[%d] Rule = %q, want %q", i, f.Rule, RuleOpenClarification)
				}
				if f.Entity != "F-0001" || f.File != d.rel {
					t.Errorf("[%d] Entity/File = %q/%q, want F-0001/%s", i, f.Entity, f.File, d.rel)
				}
				if !strings.Contains(f.Detail, tt.want[i]) {
					t.Errorf("[%d] Detail = %q, want substring %q", i, f.Detail, tt.want[i])
				}
			}
		})
	}
}

// TestOpenClarificationRunScopesToLiveEntities pins the wiring: Run flags the
// open marker in a live feature's doc and stays silent for the same marker in
// an implemented (terminal) feature's doc.
func TestOpenClarificationRunScopesToLiveEntities(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	features := []store.Feature{
		{ID: "F-0001", Title: "Viva com pergunta aberta", State: domain.StateInDevelopment},
		{ID: "F-0002", Title: "Terminal com pergunta aberta", State: domain.StateImplemented},
	}
	for _, f := range features {
		f.CreatedAt, f.UpdatedAt = now, now
		if err := store.SaveFeature(dir, f); err != nil {
			t.Fatalf("SaveFeature: %v", err)
		}
	}
	for _, id := range []string{"F-0001", "F-0002"} {
		docDir := filepath.Join(dir, "content", "features", id)
		if err := os.MkdirAll(docDir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		doc := "# Spec\n\nAlgo [ESCLARECER: pergunta pendente?] no texto.\n"
		if err := os.WriteFile(filepath.Join(docDir, "spec.md"), []byte(doc), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	findings, err := Run(dir)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []Finding
	for _, f := range findings {
		if f.Rule == RuleOpenClarification {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("open-clarification findings = %d, want 1: %+v", len(got), got)
	}
	if got[0].Entity != "F-0001" {
		t.Errorf("Entity = %q, want F-0001 (live feature only)", got[0].Entity)
	}
	if !strings.Contains(got[0].Detail, "pergunta pendente?") {
		t.Errorf("Detail = %q, want the question text", got[0].Detail)
	}
}

// TestClarificationUnescapedPipeInTable pins the F-0282 rule: inside a GFM
// table row an unescaped pipe is a cell separator, so a marker carrying one is
// sliced across cells and its answer chip never renders — the badge counts a
// question the page cannot answer. The same marker in prose is the current,
// working syntax and stays silent.
func TestClarificationUnescapedPipeInTable(t *testing.T) {
	d := scannedDoc{entity: "F-0001", rel: "content/features/F-0001/spec.md"}
	const header = "| Fork | Razão |\n| --- | --- |\n"

	tests := []struct {
		name string
		text string
		want int // findings of RuleClarifyUnescapedPipe
	}{
		{
			name: "raw option pipes in a table cell",
			text: header + "| [ESCLARECER: qual formato? | tabela | lista] | pq |\n",
			want: 1,
		},
		{
			name: "escaped option pipes in a table cell are fine",
			text: header + "| [ESCLARECER: qual formato? \\| tabela \\| lista] | pq |\n",
			want: 0,
		},
		{
			name: "raw option pipes in prose are the current syntax",
			text: "Texto [ESCLARECER: qual formato? | tabela | lista] fim.\n",
			want: 0,
		},
		{
			name: "marker without pipes in a table cell is fine",
			text: header + "| [ESCLARECER: qual formato?] | pq |\n",
			want: 0,
		},
		{
			// A doubled backslash escapes itself, so the pipe after it still
			// separates cells — a "strip the escaped pipes" pass would miss it.
			name: "a doubled backslash does not shield the pipe that follows",
			text: header + "| [ESCLARECIDO: qual? → a\\\\|b] | pq |\n",
			want: 1,
		},
		{
			name: "resolved marker with a raw pipe in a table cell",
			text: header + "| [ESCLARECIDO: qual? → tabela | lista] | pq |\n",
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []Finding
			for _, f := range clarificationFindings(d, tt.text) {
				if f.Rule == RuleClarifyUnescapedPipe {
					got = append(got, f)
				}
			}
			if len(got) != tt.want {
				t.Fatalf("clarify-unescaped-pipe findings = %d, want %d: %+v", len(got), tt.want, got)
			}
			for _, f := range got {
				if f.Entity != "F-0001" || f.File != d.rel {
					t.Errorf("Entity/File = %q/%q, want F-0001/%s", f.Entity, f.File, d.rel)
				}
				if !strings.Contains(f.Detail, `\|`) {
					t.Errorf("Detail = %q, want it to name the escape", f.Detail)
				}
			}
		})
	}
}

// TestClarifyUnescapedPipe_InRuleOrderAndRank pins the rule's registration:
// without it the finding would never reach the report.
func TestClarifyUnescapedPipe_InRuleOrderAndRank(t *testing.T) {
	var found bool
	for _, r := range RuleOrder {
		if r == RuleClarifyUnescapedPipe {
			found = true
		}
	}
	if !found {
		t.Fatalf("RuleClarifyUnescapedPipe ausente de RuleOrder")
	}
	if _, ok := ruleRank[RuleClarifyUnescapedPipe]; !ok {
		t.Fatalf("RuleClarifyUnescapedPipe ausente de ruleRank")
	}
}

// TestClarificationFindings_MultiLineMarker pins F-0317 criterion 4: an open
// marker broken across a soft line break is one finding, question on one line.
func TestClarificationFindings_MultiLineMarker(t *testing.T) {
	d := scannedDoc{entity: "F-0001", rel: "content/features/F-0001/spec.md"}
	got := clarificationFindings(d, "x [ESCLARECER: qual\n  formato usar?] y")
	if len(got) != 1 || !strings.Contains(got[0].Detail, "qual formato usar?") {
		t.Fatalf("findings = %+v, want one with the normalized question", got)
	}
}
