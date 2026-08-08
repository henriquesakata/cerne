package room

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMinutesMarkdownGolden(t *testing.T) {
	r := Room{
		ID:    "R1",
		Topic: "deploy quebrado",
		Minutes: &Minutes{
			Conclusion:       "Reverter o deploy e investigar amanhã.",
			Rationale:        "O rollback é mais rápido que um fix ao vivo sob pressão.",
			DiscardedOptions: []string{"hotfix ao vivo", "esperar até segunda"},
			Owners:           []string{"alice"},
			NextSteps:        []string{"abrir post-mortem", "revisar alertas de canary"},
		},
	}
	want := "# Ata — R1 (deploy quebrado)\n\n" +
		"## Conclusão\n\nReverter o deploy e investigar amanhã.\n\n" +
		"## Justificativa\n\nO rollback é mais rápido que um fix ao vivo sob pressão.\n\n" +
		"## Opções descartadas\n\n- hotfix ao vivo\n- esperar até segunda\n\n" +
		"## Responsáveis\n\n- alice\n\n" +
		"## Próximos passos\n\n- abrir post-mortem\n- revisar alertas de canary\n\n"
	got := RenderMinutesMarkdown(r)
	if got != want {
		t.Fatalf("RenderMinutesMarkdown =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderMinutesMarkdownEmptyListsShowPlaceholder(t *testing.T) {
	r := Room{ID: "R1", Topic: "t", Minutes: &Minutes{Conclusion: "ok", Rationale: "ok"}}
	got := RenderMinutesMarkdown(r)
	want := "# Ata — R1 (t)\n\n" +
		"## Conclusão\n\nok\n\n" +
		"## Justificativa\n\nok\n\n" +
		"## Opções descartadas\n\n_Nenhum._\n\n" +
		"## Responsáveis\n\n_Nenhum._\n\n" +
		"## Próximos passos\n\n_Nenhum._\n\n"
	if got != want {
		t.Fatalf("RenderMinutesMarkdown =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderMinutesMarkdownShowsOpenItemsException(t *testing.T) {
	r := Room{ID: "R1", Topic: "t", Minutes: &Minutes{Conclusion: "ok", Rationale: "ok", OpenItems: []string{"I3", "I5"}}}
	got := RenderMinutesMarkdown(r)
	if !strings.Contains(got, "## Itens deixados em aberto (exceção registrada)\n\n- I3\n- I5\n\n") {
		t.Fatalf("RenderMinutesMarkdown missing open-items exception section: %s", got)
	}
}

func TestRenderMinutesMarkdownNilReturnsEmpty(t *testing.T) {
	if got := RenderMinutesMarkdown(Room{ID: "R1", Topic: "t"}); got != "" {
		t.Fatalf("RenderMinutesMarkdown = %q, want empty for a room with no minutes", got)
	}
}

// F-0238: the note body is the same projection plus provenance (F-0238
// criterion 7) — room, objective, start/end + duration, message count and
// participants (nick, bio, joined_at).

func TestRenderMinutesNoteBodyGolden(t *testing.T) {
	opened := time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC)
	closed := time.Date(2026, 8, 7, 14, 32, 0, 0, time.UTC)
	registered := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	r := Room{
		ID:           "R3",
		Topic:        "deploy quebrado",
		OpenedAt:     opened,
		ClosedAt:     &closed,
		MessageCount: 12,
		Participants: []Participant{
			{Nick: "alice", Bio: "facilitadora", JoinedAt: opened.Add(5 * time.Second)},
			{Nick: "bob", JoinedAt: opened.Add(70 * time.Second)},
		},
		Minutes: &Minutes{
			Conclusion: "Reverter o deploy.",
			Rationale:  "Mais rápido.",
		},
	}
	got := RenderMinutesNoteBody(r, registered)

	for _, want := range []string{
		"# Ata — R3: deploy quebrado\n",
		"## Proveniência\n",
		"- Sala: R3\n",
		"- Objetivo: deploy quebrado\n",
		"- Início: " + opened.Format(time.RFC3339) + "\n",
		"- Fim: " + closed.Format(time.RFC3339),
		"- Mensagens: #1–#12\n",
		"- Registrado em: " + registered.Format(time.RFC3339) + "\n",
		"## Participantes\n",
		"- alice — facilitadora (entrou às " + opened.Add(5*time.Second).Format(time.RFC3339) + ")\n",
		"- bob (entrou às " + opened.Add(70*time.Second).Format(time.RFC3339) + ")\n",
		"## Conclusão\n\nReverter o deploy.\n\n",
		"## Justificativa\n\nMais rápido.\n\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("RenderMinutesNoteBody missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderMinutesNoteBodyNilReturnsEmpty(t *testing.T) {
	if got := RenderMinutesNoteBody(Room{ID: "R1", Topic: "t"}, time.Now()); got != "" {
		t.Fatalf("RenderMinutesNoteBody = %q, want empty for a room with no minutes", got)
	}
}
