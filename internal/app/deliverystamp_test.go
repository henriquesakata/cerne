package app

import (
	"strings"
	"testing"
)

const (
	stampCommit = "https://github.com/henriquesakata/cerne/commit/d2f4a4d8cabcdef"
	stampPR     = "https://github.com/henriquesakata/cerne/pull/12"
)

// F-0355 crit. 4: the block is appended, then replaced in place — one block
// only, with both links, and the same input stamps byte-identically.
func TestStampDeliveryAppendsAndReplaces(t *testing.T) {
	res := "---\ncerne_id: F-1\n---\n\n# Result\n\ntexto\n"
	once := StampDelivery(res, stampCommit, stampPR)
	want := "- Commit: [`d2f4a4d8c`](" + stampCommit + ")\n- PR: [henriquesakata/cerne#12](" + stampPR + ")\n"
	if !strings.HasSuffix(once, "## Entrega\n\n"+want) {
		t.Fatalf("block missing or malformed:\n%s", once)
	}
	if StampDelivery(once, stampCommit, stampPR) != once {
		t.Fatal("re-stamping the same link must be byte-identical")
	}
	other := StampDelivery(once, stampCommit, "https://github.com/henriquesakata/cerne/pull/13")
	if strings.Count(other, "## Entrega") != 1 || !strings.Contains(other, "cerne#13") || strings.Contains(other, "cerne#12") {
		t.Fatalf("the block must be replaced, not duplicated:\n%s", other)
	}
}

// F-0355 crit. 4: the block goes before "## Custo", and a stamp with the
// sections after it keeps them.
func TestStampDeliveryBeforeCusto(t *testing.T) {
	res := "# R\n\ntexto\n\n## Custo\n\ntokens\n"
	got := StampDelivery(res, stampCommit, "")
	i, j := strings.Index(got, "## Entrega"), strings.Index(got, "## Custo")
	if i < 0 || j < 0 || i > j {
		t.Fatalf("Entrega must come before Custo:\n%s", got)
	}
	if !strings.HasSuffix(got, "## Custo\n\ntokens\n") || strings.Contains(got, "- PR:") {
		t.Fatalf("Custo kept and no PR line without a PR:\n%s", got)
	}
	// Replacing in the middle keeps what follows.
	mid := "# R\n\n## Entrega\n\n- Commit: [`old`](x)\n\n## Custo\n\ntokens\n"
	got = StampDelivery(mid, stampCommit, stampPR)
	if strings.Contains(got, "old") || !strings.HasSuffix(got, "## Custo\n\ntokens\n") || strings.Count(got, "## Entrega") != 1 {
		t.Fatalf("middle block not replaced cleanly:\n%s", got)
	}
}

// F-0355 crit. 4: a fenced "## Entrega" is an example, not the block.
func TestStampDeliveryIgnoresFence(t *testing.T) {
	res := "# R\n\n```md\n## Entrega\n\n- Commit: [`x`](y)\n```\n"
	got := StampDelivery(res, stampCommit, stampPR)
	if !strings.HasPrefix(got, res) || strings.Count(got, "## Entrega") != 2 {
		t.Fatalf("the fenced example must be untouched and the real block added:\n%s", got)
	}
}

func TestStampDeliveryNoLink(t *testing.T) {
	if got := StampDelivery("# R\n", "", ""); got != "# R\n" {
		t.Fatalf("no link must leave the result alone, got %q", got)
	}
}

func TestDeliveryBlockLabels(t *testing.T) {
	cases := map[string]string{
		"https://gitlab.com/rockt/cerne/-/merge_requests/7": "[rockt/cerne#7]",
		"https://git.rockt.com.br/rockt/hub/pulls/2150":     "[rockt/hub#2150]",
		"https://example.com/odd":                           "[https://example.com/odd]",
	}
	for pr, want := range cases {
		if got := deliveryBlock("", pr); !strings.Contains(got, want) {
			t.Errorf("%s: %q lacks %s", pr, got, want)
		}
	}
}
