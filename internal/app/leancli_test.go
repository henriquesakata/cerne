package app

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// originCommits counts the commits on the bare backend's HEAD.
func originCommits(t *testing.T, origin string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", origin, "rev-list", "--count", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func mkCap(t *testing.T, repoDir, projDir, title string) string {
	t.Helper()
	c, err := CreateCapability(repoDir, projDir, title, "")
	if err != nil {
		t.Fatalf("CreateCapability: %v", err)
	}
	return c.ID
}

// F-0514 crit. 1
func TestCreateLikeWithCapabilitiesOneCommit(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	cid := mkCap(t, repoDir, projDir, "Cap")
	before := originCommits(t, origin)
	e, err := CreateLike(repoDir, projDir, "E", "Épico", "", "", "", nil, nil, []string{strings.ToLower(cid), cid}, "", false, "", "", "")
	if err != nil {
		t.Fatalf("CreateLike: %v", err)
	}
	if got := originCommits(t, origin) - before; got != 1 {
		t.Fatalf("commits = %d, want 1", got)
	}
	if len(e.Capabilities) != 1 || e.Capabilities[0] != cid {
		t.Fatalf("capabilities = %v, want [%s]", e.Capabilities, cid)
	}
	tk, err := CreateLike(repoDir, projDir, "T", "Passo", "", e.ID, "", nil, nil, []string{cid}, "", false, "", "", "")
	if err == nil && len(tk.Capabilities) != 1 {
		t.Fatalf("task capabilities = %v", tk.Capabilities)
	}
}

// F-0514 crit. 1
func TestCreateLikeBadCapabilityCreatesNothing(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	cid := mkCap(t, repoDir, projDir, "Cap")
	old := mkCap(t, repoDir, projDir, "Old")
	if _, err := RetireCapability(repoDir, projDir, old, "gone"); err != nil {
		t.Fatalf("retire: %v", err)
	}
	before := originCommits(t, origin)
	for _, bad := range []string{"C-9999", old} {
		_, err := CreateLike(repoDir, projDir, "F", "Nasce", "", "", "", nil, nil, []string{cid, bad}, "", false, "", "", "")
		if !errors.Is(err, ErrUnknownCapability) {
			t.Fatalf("%s: err = %v, want ErrUnknownCapability", bad, err)
		}
	}
	if got := originCommits(t, origin); got != before {
		t.Fatalf("commits moved %d -> %d on a refused creation", before, got)
	}
	// NextID did not advance and a retry with the same title is not a duplicate.
	f, err := CreateLike(repoDir, projDir, "F", "Nasce", "", "", "", nil, nil, []string{cid}, "", false, "", "", "")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if f.ID != "F-0001" {
		t.Fatalf("id = %s, want F-0001 (NextID must not advance)", f.ID)
	}
}

func mkFeatures(t *testing.T, repoDir, projDir string, titles ...string) []string {
	t.Helper()
	var ids []string
	for _, ti := range titles {
		f, err := CreateFeature(repoDir, projDir, ti, "", "", nil, nil)
		if err != nil {
			t.Fatalf("CreateFeature: %v", err)
		}
		ids = append(ids, f.ID)
	}
	return ids
}

// F-0514 crit. 2
func TestLinkFeaturesCapabilitiesBatchOneCommit(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	cid := mkCap(t, repoDir, projDir, "Cap")
	ids := mkFeatures(t, repoDir, projDir, "Alpha card", "Beta thing")
	before := originCommits(t, origin)
	got, err := LinkFeaturesCapabilities(repoDir, projDir, []string{strings.ToLower(ids[0]), ids[1], ids[0]}, []string{strings.ToLower(cid)}, false)
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("updated = %d, want 2 (deduplicated)", len(got))
	}
	if n := originCommits(t, origin) - before; n != 1 {
		t.Fatalf("commits = %d, want 1", n)
	}
	for _, id := range ids {
		f, _ := store.LoadFeature(repoDir, id)
		if len(f.Capabilities) != 1 || f.Capabilities[0] != cid {
			t.Fatalf("%s capabilities = %v", id, f.Capabilities)
		}
	}
	before = originCommits(t, origin)
	if _, err := LinkFeaturesCapabilities(repoDir, projDir, ids, nil, true); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if n := originCommits(t, origin) - before; n != 1 {
		t.Fatalf("clear commits = %d, want 1", n)
	}
	f, _ := store.LoadFeature(repoDir, ids[1])
	if len(f.Capabilities) != 0 {
		t.Fatalf("not cleared: %v", f.Capabilities)
	}
}

// F-0514 crit. 2
func TestLinkFeaturesCapabilitiesAbortsWholeBatch(t *testing.T) {
	repoDir, projDir, origin := newFeatureBackend(t)
	cid := mkCap(t, repoDir, projDir, "Cap")
	ids := mkFeatures(t, repoDir, projDir, "Alpha card")
	before := originCommits(t, origin)
	if _, err := LinkFeaturesCapabilities(repoDir, projDir, []string{ids[0], "F-9999"}, []string{cid}, false); err == nil {
		t.Fatal("want an error for the unknown feature")
	}
	if _, err := LinkFeaturesCapabilities(repoDir, projDir, ids, []string{"C-9999"}, false); !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("err = %v, want ErrUnknownCapability", err)
	}
	if got := originCommits(t, origin); got != before {
		t.Fatalf("commits moved on a refused batch")
	}
	f, _ := store.LoadFeature(repoDir, ids[0])
	if len(f.Capabilities) != 0 {
		t.Fatalf("partial mutation: %v", f.Capabilities)
	}
}
