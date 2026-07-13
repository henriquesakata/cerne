package clarify

import (
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
