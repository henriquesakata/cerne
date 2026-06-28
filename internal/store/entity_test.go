package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
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
	if err := SaveFeature(dir, want); err != nil {
		t.Fatalf("SaveFeature: %v", err)
	}
	got, err := LoadFeature(dir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.ID != want.ID || got.Title != want.Title || got.Module != want.Module ||
		got.State != want.State || got.SupersededBy != want.SupersededBy || got.Reason != want.Reason ||
		got.Parent != want.Parent {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
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
	if !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("times: got %v / %v, want %v / %v",
			got.CreatedAt, got.UpdatedAt, want.CreatedAt, want.UpdatedAt)
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
