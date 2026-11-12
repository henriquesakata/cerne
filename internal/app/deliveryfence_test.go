package app

import (
	"strings"
	"testing"
)

// F-0355 A4: a four-backtick fence holding a three-backtick line stays open
// until a four-backtick closer; the "## Entrega" inside it is an example, and
// the prose after the real heading is not eaten.
func TestStampDeliveryCommonMarkFences(t *testing.T) {
	res := "# R\n\n````md\n```\n## Entrega\n\n- Commit: [`x`](y)\n```\n## Custo\n````\n\ntexto depois\n\n## Entrega\n\n- Commit: [`old`](o)\n\n## Notas\n\nprosa final\n"
	got := StampDelivery(res, stampCommit, stampPR)
	if !strings.HasPrefix(got, "# R\n\n````md\n```\n## Entrega\n\n- Commit: [`x`](y)\n```\n## Custo\n````\n\ntexto depois\n\n") {
		t.Fatalf("the fenced example was touched:\n%s", got)
	}
	if strings.Contains(got, "`old`") || !strings.Contains(got, "cerne#12") || !strings.Contains(got, "prosa final") {
		t.Fatalf("real block not replaced, or prose lost:\n%s", got)
	}
	// A tilde fence is not closed by backticks, and a closer cannot carry an info string.
	res = "# R\n\n~~~\n```\n## Entrega\n~~~ not a closer\n## Entrega\n~~~\n\nfim\n"
	if got := StampDelivery(res, stampCommit, ""); !strings.HasPrefix(got, res[:len(res)-len("\nfim\n")]) || !strings.Contains(got, "fim") {
		t.Fatalf("tilde fence mishandled:\n%s", got)
	}
}
