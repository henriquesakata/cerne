package app

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

func readResult(t *testing.T, repoDir, id string) string {
	t.Helper()
	// The committed result (HEAD of the backend clone), not the working tree.
	if out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output(); err != nil || len(out) != 0 {
		t.Fatalf("backend clone not clean: %v\n%s", err, out)
	}
	raw, err := exec.Command("git", "-C", repoDir, "show", "HEAD:content/features/"+id+"/result.md").Output()
	if err != nil {
		t.Fatalf("read committed result: %v", err)
	}
	return string(raw)
}

// F-0355 crit. 4: complete with a delivery writes the "## Entrega" block, with
// the commit and the PR as links, in the same transition.
func TestCompleteStampsEntregaBlock(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")

	d := &Delivery{
		CommitURL: "https://github.com/o/r/commit/abc1234def",
		MRURL:     "https://github.com/o/r/pull/7",
	}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", d); err != nil {
		t.Fatalf("complete: %v", err)
	}
	res := readResult(t, repoDir, "F-0001")
	for _, want := range []string{"## Entrega", "- Commit: [`abc1234de`](https://github.com/o/r/commit/abc1234def)", "- PR: [o/r#7](https://github.com/o/r/pull/7)"} {
		if !strings.Contains(res, want) {
			t.Fatalf("result lacks %q:\n%s", want, res)
		}
	}
}

// F-0355 crit. 4, 8: a complete with no delivery leaves the result byte-equal.
func TestCompleteWithoutDeliveryLeavesResult(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	before := readResult(t, repoDir, "F-0001")
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if after := readResult(t, repoDir, "F-0001"); after != before {
		t.Fatalf("result changed without a delivery:\n%s", after)
	}
}

// F-0355 crit. 5: deliver on an implemented feature rewrites the block (one
// block, the new link).
func TestDeliverRewritesEntregaOnImplemented(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	first := &Delivery{CommitURL: "https://github.com/o/r/commit/aaaaaaa", MRURL: "https://github.com/o/r/pull/7"}
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", first); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if err := SetFeatureDelivery(repoDir, projDir, "F-0001", Delivery{CommitURL: "https://github.com/o/r/commit/bbbbbbb", MRURL: "https://github.com/o/r/pull/8"}); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	res := readResult(t, repoDir, "F-0001")
	if strings.Count(res, "## Entrega") != 1 || !strings.Contains(res, "bbbbbbb") || !strings.Contains(res, "o/r#8") || strings.Contains(res, "aaaaaaa") || strings.Contains(res, "o/r#7") {
		t.Fatalf("block not rewritten in place:\n%s", res)
	}
}

// F-0355 crit. 5: deliver on a done feature records the data and leaves the
// result byte-equal.
func TestDeliverOnDoneWritesDataOnly(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateDone, "spec.md", "plan.md", "result.md")
	before := readResult(t, repoDir, "F-0001")
	d := Delivery{CommitURL: "https://github.com/o/r/commit/abc1234", MRURL: "https://github.com/o/r/pull/7"}
	if err := SetFeatureDelivery(repoDir, projDir, "F-0001", d); err != nil {
		t.Fatalf("deliver on done: %v", err)
	}
	if after := readResult(t, repoDir, "F-0001"); after != before {
		t.Fatalf("a done result must stay byte-equal, got:\n%s", after)
	}
	f, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil || f.DeliveryMRURL != d.MRURL || f.DeliveryCommitURL != d.CommitURL {
		t.Fatalf("data not recorded: %+v %v", f, err)
	}
}
