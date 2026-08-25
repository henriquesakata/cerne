package clarify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []Clarification
	}{
		{
			name:    "no markers",
			content: "prosa comum sem marcador algum, nem [[F-0123]] engana.",
			want:    nil,
		},
		{
			name:    "bare marker",
			content: "algo incerto [ESCLARECER] aqui",
			want: []Clarification{
				{Raw: "[ESCLARECER]", Offset: 13},
			},
		},
		{
			name:    "question",
			content: "x [ESCLARECER: qual formato usar?] y",
			want: []Clarification{
				{Question: "qual formato usar?", Raw: "[ESCLARECER: qual formato usar?]", Offset: 2},
			},
		},
		{
			name:    "question with options",
			content: "[ESCLARECER: onde persistir? | in-place no doc | sidecar JSON]",
			want: []Clarification{
				{
					Question: "onde persistir?",
					Options:  []string{"in-place no doc", "sidecar JSON"},
					Raw:      "[ESCLARECER: onde persistir? | in-place no doc | sidecar JSON]",
					Offset:   0,
				},
			},
		},
		{
			name:    "legacy guillemets",
			content: "veja [«ESCLARECER»: pergunta antiga] no doc",
			want: []Clarification{
				{Question: "pergunta antiga", Raw: "[«ESCLARECER»: pergunta antiga]", Offset: 5},
			},
		},
		{
			name:    "legacy guillemets bare",
			content: "[«ESCLARECER»]",
			want: []Clarification{
				{Raw: "[«ESCLARECER»]", Offset: 0},
			},
		},
		{
			name:    "resolved",
			content: "[ESCLARECIDO: onde persistir? → in-place no doc]",
			want: []Clarification{
				{
					Question: "onde persistir?",
					Resolved: true,
					Answer:   "in-place no doc",
					Raw:      "[ESCLARECIDO: onde persistir? → in-place no doc]",
					Offset:   0,
				},
			},
		},
		{
			name:    "resolved without arrow keeps whole body as answer",
			content: "[ESCLARECIDO: usar pipe inline]",
			want: []Clarification{
				{
					Resolved: true,
					Answer:   "usar pipe inline",
					Raw:      "[ESCLARECIDO: usar pipe inline]",
					Offset:   0,
				},
			},
		},
		{
			name:    "multiple markers with exact offsets",
			content: "a [ESCLARECER: um?] b [ESCLARECER: dois?] c",
			want: []Clarification{
				{Question: "um?", Raw: "[ESCLARECER: um?]", Offset: 2},
				{Question: "dois?", Raw: "[ESCLARECER: dois?]", Offset: 22},
			},
		},
		{
			name:    "identical markers get distinct offsets",
			content: "[ESCLARECER: igual?] meio [ESCLARECER: igual?]",
			want: []Clarification{
				{Question: "igual?", Raw: "[ESCLARECER: igual?]", Offset: 0},
				{Question: "igual?", Raw: "[ESCLARECER: igual?]", Offset: 26},
			},
		},
		{
			name:    "prefix word is not a marker",
			content: "[ESCLARECEDOR] não é marcador; [ESCLARECIMENTO: x] também não.",
			want:    nil,
		},
		{
			name:    "marker does not span lines",
			content: "[ESCLARECER: quebrou\naqui]",
			want:    nil,
		},
		{
			name:    "empty question with options still yields options",
			content: "[ESCLARECER: | a | b]",
			want: []Clarification{
				{Options: []string{"a", "b"}, Raw: "[ESCLARECER: | a | b]", Offset: 0},
			},
		},
		{
			name:    "marker inside html comment is template guidance, not authored",
			content: "<!-- Marque incerteza com [ESCLARECER: pergunta específica] e siga. -->\nprosa",
			want:    nil,
		},
		{
			name:    "marker after a comment keeps its exact offset",
			content: "<!-- guia [ESCLARECER] -->\n[ESCLARECER: real?]",
			want: []Clarification{
				{Question: "real?", Raw: "[ESCLARECER: real?]", Offset: 27},
			},
		},
		{
			name:    "unclosed comment swallows to eof",
			content: "texto <!-- aberto [ESCLARECER: sumiu?]",
			want:    nil,
		},
		{
			name:    "marker in inline code is a citation, not authored",
			content: "docs citam `[ESCLARECER: exemplo]` na prosa",
			want:    nil,
		},
		{
			name:    "marker in fenced code block is a citation",
			content: "```\n[ESCLARECER: dentro do fence]\n```\nprosa",
			want:    nil,
		},
		{
			name:    "marker after inline code keeps offset",
			content: "`code` [ESCLARECER: real?]",
			want: []Clarification{
				{Question: "real?", Raw: "[ESCLARECER: real?]", Offset: 7},
			},
		},
		// F-0182: the en vocabulary is part of the catalog — same shapes,
		// same rules. Without it the approve gate is fail-open by language:
		// an English spec full of open questions sails through.
		{
			name:    "en open with question",
			content: "x [CLARIFY: which format?] y",
			want: []Clarification{
				{Question: "which format?", Raw: "[CLARIFY: which format?]", Offset: 2},
			},
		},
		{
			name:    "en open with options",
			content: "[CLARIFY: where? | doc | sidecar]",
			want: []Clarification{
				{
					Question: "where?",
					Options:  []string{"doc", "sidecar"},
					Raw:      "[CLARIFY: where? | doc | sidecar]",
					Offset:   0,
				},
			},
		},
		{
			name:    "en bare",
			content: "unclear [CLARIFY] here",
			want: []Clarification{
				{Raw: "[CLARIFY]", Offset: 8},
			},
		},
		{
			name:    "en resolved with arrow",
			content: "[CLARIFIED: where? → in the doc]",
			want: []Clarification{
				{
					Question: "where?",
					Resolved: true,
					Answer:   "in the doc",
					Raw:      "[CLARIFIED: where? → in the doc]",
					Offset:   0,
				},
			},
		},
		{
			name:    "en prefix word is not a marker",
			content: "[CLARIFIER] is not a marker; [CLARIFICATION: x] neither.",
			want:    nil,
		},
		{
			name:    "en marker in citation contexts is not authored",
			content: "cite `[CLARIFY: inline]` and\n```\n[CLARIFY: fenced]\n```\n<!-- [CLARIFY: guide] -->",
			want:    nil,
		},
		{
			name:    "mixed vocabularies coexist in one doc",
			content: "a [ESCLARECER: um?] b [CLARIFY: two?] c",
			want: []Clarification{
				{Question: "um?", Raw: "[ESCLARECER: um?]", Offset: 2},
				{Question: "two?", Raw: "[CLARIFY: two?]", Offset: 22},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("Parse() returned %d markers, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].Question != tt.want[i].Question {
					t.Errorf("[%d] Question = %q, want %q", i, got[i].Question, tt.want[i].Question)
				}
				if strings.Join(got[i].Options, "‖") != strings.Join(tt.want[i].Options, "‖") {
					t.Errorf("[%d] Options = %v, want %v", i, got[i].Options, tt.want[i].Options)
				}
				if got[i].Resolved != tt.want[i].Resolved {
					t.Errorf("[%d] Resolved = %v, want %v", i, got[i].Resolved, tt.want[i].Resolved)
				}
				if got[i].Answer != tt.want[i].Answer {
					t.Errorf("[%d] Answer = %q, want %q", i, got[i].Answer, tt.want[i].Answer)
				}
				if got[i].Offset != tt.want[i].Offset {
					t.Errorf("[%d] Offset = %d, want %d", i, got[i].Offset, tt.want[i].Offset)
				}
				if got[i].Raw != tt.want[i].Raw {
					t.Errorf("[%d] Raw = %q, want %q", i, got[i].Raw, tt.want[i].Raw)
				}
				if tt.content[got[i].Offset:got[i].Offset+len(got[i].Raw)] != got[i].Raw {
					t.Errorf("[%d] Offset/Raw mismatch: content slice = %q", i, tt.content[got[i].Offset:got[i].Offset+len(got[i].Raw)])
				}
			}
		})
	}
}

// TestParseCorpus is the Go side of the Go↔TS parity lock (F-0182): the same
// testdata/corpus.json is read by ui/src/Clarify.test.tsx, and both parsers
// must return identical span sets. A vocabulary added on one side only fails
// the other side's run of this corpus.
func TestParseCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "corpus.json"))
	if err != nil {
		t.Fatalf("reading corpus: %v", err)
	}
	var corpus struct {
		Cases []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
			Spans   []struct {
				Raw      string `json:"raw"`
				Offset   int    `json:"offset"`
				Resolved bool   `json:"resolved"`
			} `json:"spans"`
			// Optional (F-0282): when present, both parsers must also agree on
			// the parsed question and options of the FIRST span — the escaped
			// pipe form is only locked by comparing what was read, not spans.
			Question *string   `json:"question"`
			Options  *[]string `json:"options"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("corpus json: %v", err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("corpus has no cases")
	}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got := Parse(c.Content)
			if len(got) != len(c.Spans) {
				t.Fatalf("Parse() returned %d spans, want %d: %+v", len(got), len(c.Spans), got)
			}
			for i, want := range c.Spans {
				if got[i].Raw != want.Raw || got[i].Offset != want.Offset || got[i].Resolved != want.Resolved {
					t.Errorf("[%d] = {%q %d %v}, want {%q %d %v}",
						i, got[i].Raw, got[i].Offset, got[i].Resolved, want.Raw, want.Offset, want.Resolved)
				}
			}
			if c.Question != nil && got[0].Question != *c.Question {
				t.Errorf("Question = %q, want %q", got[0].Question, *c.Question)
			}
			if c.Options != nil && strings.Join(got[0].Options, "‖") != strings.Join(*c.Options, "‖") {
				t.Errorf("Options = %v, want %v", got[0].Options, *c.Options)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		content string
		answer  string
		want    string
		ok      bool
	}{
		{
			name:    "pt question",
			content: "[ESCLARECER: qual backend?]",
			answer:  "S3",
			want:    "[ESCLARECIDO: qual backend? → S3]",
			ok:      true,
		},
		{
			name:    "pt bare keeps answer only",
			content: "[ESCLARECER]",
			answer:  "usar pipe",
			want:    "[ESCLARECIDO: usar pipe]",
			ok:      true,
		},
		{
			name:    "pt options collapse to the chosen answer",
			content: "[ESCLARECER: onde? | in-place | sidecar]",
			answer:  "in-place",
			want:    "[ESCLARECIDO: onde? → in-place]",
			ok:      true,
		},
		{
			name:    "en stays en",
			content: "[CLARIFY: which format?]",
			answer:  "json",
			want:    "[CLARIFIED: which format? → json]",
			ok:      true,
		},
		{
			name:    "legacy guillemets resolve without them",
			content: "[«ESCLARECER»: pergunta antiga]",
			answer:  "resposta",
			want:    "[ESCLARECIDO: pergunta antiga → resposta]",
			ok:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := Parse(tt.content)
			if len(cs) != 1 {
				t.Fatalf("Parse() returned %d markers, want 1", len(cs))
			}
			got, ok := Resolve(cs[0], tt.answer)
			if ok != tt.ok {
				t.Fatalf("Resolve() ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveRefusesResolvedMarker(t *testing.T) {
	cs := Parse("[ESCLARECIDO: onde? → no doc]")
	if len(cs) != 1 {
		t.Fatalf("Parse() returned %d markers, want 1", len(cs))
	}
	if got, ok := Resolve(cs[0], "de novo"); ok {
		t.Errorf("Resolve() on a resolved marker = %q, ok=true; want ok=false", got)
	}
}

// TestResolveCorpus is the Go side of the resolution parity lock: the corpus'
// resolve cases are also replayed by ui/src/Clarify.test.tsx over
// resolveMarker, so the two rewriters cannot drift (same contract as
// TestParseCorpus; F-0182).
func TestResolveCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "corpus.json"))
	if err != nil {
		t.Fatalf("reading corpus: %v", err)
	}
	var corpus struct {
		Resolve []struct {
			Name    string  `json:"name"`
			Content string  `json:"content"`
			Raw     string  `json:"raw"`
			Offset  int     `json:"offset"`
			Answer  string  `json:"answer"`
			Want    *string `json:"want"`
		} `json:"resolve"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("corpus json: %v", err)
	}
	if len(corpus.Resolve) == 0 {
		t.Fatal("corpus has no resolve cases")
	}
	for _, c := range corpus.Resolve {
		t.Run(c.Name, func(t *testing.T) {
			var target *Clarification
			for _, m := range Parse(c.Content) {
				if m.Offset == c.Offset && m.Raw == c.Raw {
					target = &m
					break
				}
			}
			if target == nil {
				if c.Want != nil {
					t.Fatalf("no marker anchored at %d with raw %q", c.Offset, c.Raw)
				}
				return
			}
			marker, ok := Resolve(*target, c.Answer)
			if !ok {
				if c.Want != nil {
					t.Fatalf("Resolve() ok=false, want %q", *c.Want)
				}
				return
			}
			got := c.Content[:target.Offset] + marker + c.Content[target.Offset+len(target.Raw):]
			if c.Want == nil {
				t.Fatalf("Resolve() = %q, want refusal", got)
			}
			if got != *c.Want {
				t.Errorf("resolved content = %q, want %q", got, *c.Want)
			}
		})
	}
}

func TestOpen(t *testing.T) {
	content := "[ESCLARECER: aberta?] e [ESCLARECIDO: fechada? → sim]"
	open := Open(content)
	if len(open) != 1 {
		t.Fatalf("Open() returned %d, want 1: %+v", len(open), open)
	}
	if open[0].Question != "aberta?" {
		t.Errorf("Question = %q, want %q", open[0].Question, "aberta?")
	}
}

// F-0197: uma resposta corrigida não substitui a anterior — anexa-se a ela.
// Parse expõe a trilha inteira (Answers) e a corrente (Answer, a última).
func TestParseAnswerTrail(t *testing.T) {
	cases := []struct {
		name        string
		content     string
		wantQuestio string
		wantAnswers []string
	}{
		{"trilha de duas", "[ESCLARECIDO: onde? → no doc → no backend]", "onde?", []string{"no doc", "no backend"}},
		{"legado é trilha de um", "[ESCLARECIDO: onde? → no doc]", "onde?", []string{"no doc"}},
		{"sem pergunta, seta inicial marca a ausência", "[CLARIFIED: → use json → use yaml]", "", []string{"use json", "use yaml"}},
		{"sem pergunta, forma legada de uma resposta", "[CLARIFIED: use json]", "", []string{"use json"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := Parse(tc.content)
			if len(cs) != 1 {
				t.Fatalf("Parse() returned %d markers, want 1", len(cs))
			}
			c := cs[0]
			if c.Question != tc.wantQuestio {
				t.Errorf("Question = %q, want %q", c.Question, tc.wantQuestio)
			}
			if !slices.Equal(c.Answers, tc.wantAnswers) {
				t.Errorf("Answers = %#v, want %#v", c.Answers, tc.wantAnswers)
			}
			// Answer segue sendo a corrente: consumidor existente não muda.
			if want := tc.wantAnswers[len(tc.wantAnswers)-1]; c.Answer != want {
				t.Errorf("Answer = %q, want the current one %q", c.Answer, want)
			}
		})
	}
}

// TestCorrect cobre o inverso de Resolve: anexar à trilha, no vocabulário do
// marcador original, recusando o que não é um resolvido ancorado.
func TestCorrect(t *testing.T) {
	pt := Parse("[ESCLARECIDO: qual backend? → S3]")[0]
	got, ok := Correct(pt, "GCS")
	if !ok || got != "[ESCLARECIDO: qual backend? → S3 → GCS]" {
		t.Errorf("Correct(pt) = %q, ok=%v", got, ok)
	}
	en := Parse("[CLARIFIED: which format? → json]")[0]
	got, ok = Correct(en, "yaml")
	if !ok || got != "[CLARIFIED: which format? → json → yaml]" {
		t.Errorf("Correct(en) = %q, ok=%v; correcting must not switch vocabulary", got, ok)
	}
	open := Parse("[ESCLARECER: qual backend?]")[0]
	if got, ok := Correct(open, "S3"); ok {
		t.Errorf("Correct() on an open marker = %q, ok=true; answering is Resolve's job", got)
	}
}

// TestCorrectCorpus é o lado Go da trava de paridade da CORREÇÃO: os mesmos
// casos são reproduzidos por ui/src/Clarify.test.tsx sobre correctMarker.
func TestCorrectCorpus(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "corpus.json"))
	if err != nil {
		t.Fatalf("reading corpus: %v", err)
	}
	var corpus struct {
		Correct []struct {
			Name    string  `json:"name"`
			Content string  `json:"content"`
			Raw     string  `json:"raw"`
			Offset  int     `json:"offset"`
			Answer  string  `json:"answer"`
			Want    *string `json:"want"`
		} `json:"correct"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("corpus json: %v", err)
	}
	if len(corpus.Correct) == 0 {
		t.Fatal("corpus has no correct cases")
	}
	for _, c := range corpus.Correct {
		t.Run(c.Name, func(t *testing.T) {
			var target *Clarification
			for _, m := range Parse(c.Content) {
				if m.Offset == c.Offset && m.Raw == c.Raw {
					target = &m
					break
				}
			}
			if target == nil {
				if c.Want != nil {
					t.Fatalf("no marker anchored at %d with raw %q", c.Offset, c.Raw)
				}
				return
			}
			marker, ok := Correct(*target, c.Answer)
			if !ok {
				if c.Want != nil {
					t.Fatalf("Correct() ok=false, want %q", *c.Want)
				}
				return
			}
			got := c.Content[:target.Offset] + marker + c.Content[target.Offset+len(target.Raw):]
			if c.Want == nil {
				t.Fatalf("Correct() = %q, want refusal", got)
			}
			if got != *c.Want {
				t.Errorf("corrected content = %q, want %q", got, *c.Want)
			}
		})
	}
}
