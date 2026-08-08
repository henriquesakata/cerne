package room

import (
	"strings"
	"testing"
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
