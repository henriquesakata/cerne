package materialize

import (
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// TestContextRosterRendersDeliveryLinks (F-0179 crit. 10): a feature with
// delivery links gets clickable [commit]/[MR] markdown on its roster row.
func TestContextRosterRendersDeliveryLinks(t *testing.T) {
	backend := seedBackend(t)
	f := store.Feature{ID: "F-0001", Title: "Com entrega", State: domain.StateImplemented,
		DeliveryCommitURL: "https://gitlab.example/repo/-/commit/abc1234",
		DeliveryMRURL:     "https://gitlab.example/repo/-/merge_requests/7",
		CreatedAt:         seedTime, UpdatedAt: seedTime}
	if err := store.SaveFeature(backend, f); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	docsDir := t.TempDir()
	if _, err := Materialize(backend, docsDir, "", false); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	ctx := readContext(t, docsDir)
	var row string
	for _, line := range strings.Split(ctx, "\n") {
		if strings.Contains(line, "F-0001") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatalf("no F-0001 row in CONTEXT.md:\n%s", ctx)
	}
	if !strings.Contains(row, "[MR](https://gitlab.example/repo/-/merge_requests/7)") ||
		!strings.Contains(row, "[commit](https://gitlab.example/repo/-/commit/abc1234)") {
		t.Fatalf("roster row missing delivery links: %q", row)
	}
}
