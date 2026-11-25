package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
)

func testFeature(id string) Feature {
	return Feature{
		ID:        id,
		Title:     "Boleto import",
		Module:    "finance",
		State:     domain.StateCandidate,
		CreatedAt: time.Date(2026, 6, 1, 10, 0, 0, 123456789, time.UTC),
		UpdatedAt: time.Date(2026, 6, 2, 11, 30, 0, 0, time.UTC),
	}
}

func TestFeatureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := testFeature("F-0001")
	want.State = domain.StateSuperseded
	want.SupersededBy = "F-0002"
	want.Reason = "absorbed into bills"
	want.Parent = "F-0003"
	want.DeliveryCommitURL = "https://gitlab.example/repo/-/commit/abc1234"
	want.DeliveryMRURL = "https://gitlab.example/repo/-/merge_requests/7"
	want.Owner = "ana"
	want.Priority = "p1"
	if err := SaveFeature(dir, want); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	got, err := LoadFeature(dir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.Module != want.Module ||
		got.State != want.State || got.SupersededBy != want.SupersededBy || got.Reason != want.Reason ||
		got.Parent != want.Parent ||
		got.DeliveryCommitURL != want.DeliveryCommitURL || got.DeliveryMRURL != want.DeliveryMRURL ||
		got.Owner != want.Owner || got.Priority != want.Priority {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

// TestFeatureLoadLegacyWithoutOwnerPriority pins F-0204's criterion 6: a
// feature file written before owner/priority existed (no keys at all) loads
// cleanly with both fields at their zero value — no migration needed.
func TestFeatureLoadLegacyWithoutOwnerPriority(t *testing.T) {
	dir := t.TempDir()
	legacy := testFeature("F-0009")
	if err := SaveFeature(dir, legacy); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	path := filepath.Join(dir, "entities", "features", "F-0009.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), "owner") || strings.Contains(string(data), "priority") {
		t.Fatalf("legacy fixture unexpectedly carries owner/priority: %s", data)
	}
	got, err := LoadFeature(dir, "F-0009")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.Owner != "" || got.Priority != "" {
		t.Fatalf("got Owner=%q Priority=%q, want both empty", got.Owner, got.Priority)
	}
}

func TestDecisionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Decision{
		ID:           "D-0001",
		Title:        "Use Quarkus",
		State:        domain.DecisionSuperseded,
		SupersededBy: "D-0002",
		Reason:       "moved to native",
		Facets:       []string{"infra", "code"},
		Components:   []string{"gitea", "kpack"},
		CreatedAt:    time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC),
	}
	if err := SaveDecision(dir, want); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	got, err := LoadDecision(dir, "D-0001")
	if err != nil {
		t.Fatalf("LoadDecision: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.State != want.State ||
		got.SupersededBy != want.SupersededBy || got.Reason != want.Reason {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Facets) != 2 || got.Facets[0] != "infra" || got.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", got.Facets)
	}
	if len(got.Components) != 2 || got.Components[0] != "gitea" || got.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", got.Components)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

// E-0005 criterion 22 (T-0018): a decision carries what it informs, and a
// decision saved before the field loads and saves unchanged.
func TestDecisionInforms(t *testing.T) {
	dir := t.TempDir()
	legacy := `{"id":"D-0001","title":"Use Quarkus","state":"active","created_at":"2026-05-01T09:00:00Z","updated_at":"2026-05-01T09:00:00Z"}`
	p := filepath.Join(dir, "entities", "decisions", "D-0001.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := LoadDecision(dir, "D-0001")
	if err != nil || old.Informs != nil || old.Title != "Use Quarkus" || old.State != domain.DecisionActive {
		t.Fatalf("legacy decision = %+v, %v", old, err)
	}
	if err := SaveDecision(dir, old); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); strings.Contains(string(data), "informs") {
		t.Fatalf("a decision with no link stores an informs key:\n%s", data)
	}
	old.Informs = []string{"F-0001", "T-0002"}
	if err := SaveDecision(dir, old); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), `"informs"`) {
		t.Fatalf("informs not stored:\n%s", data)
	}
	got, err := LoadDecision(dir, "D-0001")
	if err != nil || len(got.Informs) != 2 || got.Informs[0] != "F-0001" || got.Informs[1] != "T-0002" {
		t.Fatalf("informs = %v, %v, want [F-0001 T-0002]", got.Informs, err)
	}
}

func TestReferenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Reference{
		ID:        "R-0001",
		Title:     "Keycloak OIDC docs",
		State:     domain.ReferenceCurrent,
		CreatedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 4, 1, 8, 0, 0, 0, time.UTC),
	}
	if err := SaveReference(dir, want); err != nil {
		t.Fatalf("SaveReference: %v", err)
	}
	got, err := LoadReference(dir, "R-0001")
	if err != nil {
		t.Fatalf("LoadReference: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.State != want.State {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

// F-0501 crit. 1: the check is persisted; a reference without one stays without.
func TestReferenceCheckRoundTrip(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	r := Reference{ID: "R-0001", Title: "x", State: domain.ReferenceCurrent, External: true,
		Check: &ReferenceCheck{CheckedAt: at, HTTPStatus: 200, TextHash: "aa", BaselineHash: "bb", Status: "changed"}}
	if err := SaveReference(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := LoadReference(dir, "R-0001")
	if err != nil || got.Check == nil || *got.Check != *r.Check {
		t.Fatalf("got %+v, %v; want check %+v", got.Check, err, r.Check)
	}
	plain := Reference{ID: "R-0002", Title: "y", State: domain.ReferenceCurrent}
	if err := SaveReference(dir, plain); err != nil {
		t.Fatal(err)
	}
	got, err = LoadReference(dir, "R-0002")
	if err != nil || got.Check != nil {
		t.Fatalf("got %+v, %v; want nil check", got.Check, err)
	}
	// the JSON of a reference without a check carries no "check" key
	b, _ := json.Marshal(plain)
	if strings.Contains(string(b), `"check"`) {
		t.Fatalf("check marshaled for a reference without one: %s", b)
	}
}

// F-0503 crit. 8: the copy's link, capture date, origin and status persist;
// a reference without them loads and saves back without the keys.
func TestReferenceArchiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2025, 3, 2, 10, 4, 5, 0, time.UTC)
	r := Reference{ID: "R-0001", Title: "x", State: domain.ReferenceCurrent, External: true,
		Archive: &ReferenceArchive{URL: "https://web.archive.org/web/20250302100405/https://a.org/", CapturedAt: at, Origin: "existing", Status: "archived"}}
	if err := SaveReference(dir, r); err != nil {
		t.Fatal(err)
	}
	got, err := LoadReference(dir, "R-0001")
	if err != nil || got.Archive == nil || *got.Archive != *r.Archive {
		t.Fatalf("got %+v, %v; want archive %+v", got.Archive, err, r.Archive)
	}
	// F-0503 crit. 7: the no-archive mark persists.
	off := Reference{ID: "R-0002", Title: "y", State: domain.ReferenceCurrent, NoArchive: true}
	if err := SaveReference(dir, off); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadReference(dir, "R-0002"); err != nil || !got.NoArchive {
		t.Fatalf("got NoArchive %v, %v; want true", got.NoArchive, err)
	}
	raw := `{"id":"R-0003","title":"z","state":"current","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`
	var p Reference
	if err := json.Unmarshal([]byte(raw), &p); err != nil || p.Archive != nil || p.NoArchive {
		t.Fatalf("plain = %+v, %v; want no archive fields", p, err)
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), `"archive"`) || strings.Contains(string(b), `"no_archive"`) {
		t.Fatalf("archive keys marshaled for a reference without them: %s", b)
	}
}

func TestNoteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Note{
		ID:           "N-0001",
		Title:        "Toolchain path is per-machine",
		State:        domain.NoteSuperseded,
		SupersededBy: "N-0002",
		Reason:       "corrected",
		Informs:      []string{"F-0001", "D-0003"},
		Facets:       []string{"infra", "code"},
		Components:   []string{"gitea", "kpack"},
		CreatedAt:    time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC),
	}
	if err := SaveNote(dir, want); err != nil {
		t.Fatalf("SaveNote: %v", err)
	}
	got, err := LoadNote(dir, "N-0001")
	if err != nil {
		t.Fatalf("LoadNote: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.State != want.State ||
		got.SupersededBy != want.SupersededBy || got.Reason != want.Reason {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if len(got.Informs) != 2 || got.Informs[0] != "F-0001" || got.Informs[1] != "D-0003" {
		t.Fatalf("informs = %v, want [F-0001 D-0003]", got.Informs)
	}
	if len(got.Facets) != 2 || got.Facets[0] != "infra" || got.Facets[1] != "code" {
		t.Fatalf("facets = %v, want [infra code]", got.Facets)
	}
	if len(got.Components) != 2 || got.Components[0] != "gitea" || got.Components[1] != "kpack" {
		t.Fatalf("components = %v, want [gitea kpack]", got.Components)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
	}
}

func TestLoadAllFeaturesSortedByID(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"F-0002", "F-0001"} {
		if err := SaveFeature(dir, testFeature(id)); err != nil {
			t.Fatalf("SaveFeature(%s): %v", id, err)
		}
	}
	got, err := LoadAllFeatures(dir)
	if err != nil {
		t.Fatalf("LoadAllFeatures: %v", err)
	}
	if len(got) != 2 || got[0].ID != "F-0001" || got[1].ID != "F-0002" {
		t.Fatalf("got %+v, want [F-0001, F-0002]", got)
	}
}

func TestLoadNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadFeature(dir, "F-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadFeature err = %v, want errors.Is ErrNotFound", err)
	} else if !strings.Contains(err.Error(), "F-9999") {
		t.Fatalf("LoadFeature err = %v, want id in message", err)
	}
	if _, err := LoadDecision(dir, "D-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadDecision err = %v, want errors.Is ErrNotFound", err)
	}
	if _, err := LoadReference(dir, "R-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadReference err = %v, want errors.Is ErrNotFound", err)
	}
}

func TestLoadAllAbsentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-backend-here")
	feats, err := LoadAllFeatures(dir)
	if err != nil || len(feats) != 0 {
		t.Fatalf("LoadAllFeatures = %v, %v; want empty, nil", feats, err)
	}
	decs, err := LoadAllDecisions(dir)
	if err != nil || len(decs) != 0 {
		t.Fatalf("LoadAllDecisions = %v, %v; want empty, nil", decs, err)
	}
	refs, err := LoadAllReferences(dir)
	if err != nil || len(refs) != 0 {
		t.Fatalf("LoadAllReferences = %v, %v; want empty, nil", refs, err)
	}
}

func TestSavedFileFormat(t *testing.T) {
	dir := t.TempDir()
	if err := SaveFeature(dir, testFeature("F-0001")); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "entities", "features", "F-0001.json"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(data)
	if !strings.HasPrefix(s, "{\n  \"id\"") {
		t.Fatalf("file must start with 2-space-indented id, got prefix %q", s[:min(20, len(s))])
	}
	if !strings.HasSuffix(s, "}\n") {
		t.Fatalf("file must end with closing brace + newline, got suffix %q", s[max(0, len(s)-5):])
	}
	// A feature with no parent must not carry the key (omitempty).
	if strings.Contains(s, "\"parent\"") {
		t.Fatalf("feature with no parent must omit the parent key, got:\n%s", s)
	}
}

func TestLoadRunbookContent(t *testing.T) {
	dir := t.TempDir()
	body := "# Deploy\n\n1. build\n2. ship\n"
	docDir := filepath.Join(dir, "content", "runbooks")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "RB-0001.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := LoadRunbookContent(dir, "RB-0001")
	if err != nil {
		t.Fatalf("LoadRunbookContent: %v", err)
	}
	if got != body {
		t.Fatalf("content = %q, want %q", got, body)
	}

	if _, err := LoadRunbookContent(dir, "RB-0999"); !os.IsNotExist(err) {
		t.Fatalf("missing runbook err = %v, want fs not-exist", err)
	}
}
