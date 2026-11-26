package project

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestAcceptGateRisk: "risk" is a fourth accept value, only as that literal;
// the other values keep their meaning and a near miss still reads as human.
// F-0516 crit. 1
func TestAcceptGateRisk(t *testing.T) {
	for _, tc := range []struct{ accept, want string }{
		{"risk", GateRisk},
		{"Risk", GateHuman},
		{"risky", GateHuman},
		{"merge", GateMerge},
		{"auto", GateAuto},
		{"human", GateHuman},
		{"", GateHuman},
	} {
		if got := (GatesConfig{Accept: tc.accept}).AcceptGate(); got != tc.want {
			t.Errorf("AcceptGate() with accept = %q: got %q, want %q", tc.accept, got, tc.want)
		}
	}
	g := GatesConfig{Approve: "risk", Revert: "risk"}
	if g.ApproveGate() != GateHuman || g.RevertGate() != GateHuman {
		t.Errorf("approve/revert = risk must read as human, got %q/%q", g.ApproveGate(), g.RevertGate())
	}
	// The merge gate's paths (next, accept --merged, wait, the shell hook)
	// hold a risk card too.
	for _, tc := range []struct {
		accept string
		want   bool
	}{{"merge", true}, {"risk", true}, {"auto", false}, {"human", false}, {"", false}} {
		if got := (GatesConfig{Accept: tc.accept}).AcceptsOnMerge(); got != tc.want {
			t.Errorf("AcceptsOnMerge() with accept = %q: got %v, want %v", tc.accept, got, tc.want)
		}
	}
}

// TestSettingsRegistryAcceptRisk: gates.accept takes risk, and its
// description says what risk does and lists the three conditions.
// F-0516 crit. 1, 12
func TestSettingsRegistryAcceptRisk(t *testing.T) {
	s := declared("gates.accept")
	if !slices.Contains(s.Enum, GateRisk) {
		t.Fatalf("gates.accept enum = %v, want it to hold risk", s.Enum)
	}
	for _, want := range []string{"(risk):", "sensitive", "Gauntlet", "win", "criterion", "test"} {
		if !strings.Contains(s.Description, want) {
			t.Errorf("gates.accept description lacks %q: %q", want, s.Description)
		}
	}
	projDir, _ := settingsFixture(t, gatesBlock, "", false)
	if err := SetSetting(projDir, "gates.accept", ScopeProject, "risk"); err != nil {
		t.Fatalf("SetSetting risk: %v", err)
	}
	cfg, _, err := LoadConfig(projDir)
	if err != nil || cfg.Gates.AcceptGate() != GateRisk {
		t.Errorf("AcceptGate after set risk = %q (%v), want risk", cfg.Gates.AcceptGate(), err)
	}
}

// wantRiskPaths and wantRiskFacets are the default lists (F-0516 plan).
var (
	wantRiskPaths = []string{
		".github/workflows/**", ".github/actions/**", ".gitlab-ci.yml", ".gitlab/**", ".gitea/workflows/**", ".forgejo/workflows/**",
		"go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"Cargo.toml", "Cargo.lock", "pyproject.toml", "poetry.lock", "requirements*.txt", "Gemfile*",
		"**/migrations/**", "db/migrate/**",
		".cernerc",
		".claude/**", ".cursor/**", ".codex/**", ".gemini/**", ".github/copilot-instructions.md",
		".github/hooks/**", ".github/instructions/**", ".github/skills/**",
		".mcp.json", "AGENTS.md", "CLAUDE.md", "GEMINI.md",
	}
	wantRiskFacets = []string{"auth", "infra", "data"}
)

// TestSettingsRegistryRiskLists: the two lists are declared in the process
// group, project scope, human only, as lists, with the default patterns and
// facets pinned here; unset, the gates read the defaults.
// F-0516 crit. 14
func TestSettingsRegistryRiskLists(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want []string
	}{
		{"gates.risk_human_paths", wantRiskPaths},
		{"gates.risk_human_facets", wantRiskFacets},
	} {
		if checkDeclared(tc.id, ScopeProject) != nil {
			t.Errorf("registry lacks %s in project scope", tc.id)
			continue
		}
		s := declared(tc.id)
		if s.Group != "process" || s.Type != "list" || !s.HumanOnly || !slices.Equal(s.Scopes, []string{ScopeProject}) || s.Title == "" || s.Description == "" {
			t.Errorf("%s = %+v, want process, list, human only, scope [project], a title and a description", tc.id, s)
		}
		if got, _ := s.Default.([]string); !slices.Equal(got, tc.want) {
			t.Errorf("%s default = %v, want %v", tc.id, s.Default, tc.want)
		}
		if !strings.Contains(s.Description, "An agent cannot change this key through cerne settings") {
			t.Errorf("%s description does not say an agent cannot change it: %q", tc.id, s.Description)
		}
	}
	var g GatesConfig
	if !slices.Equal(g.RiskHumanPaths(), wantRiskPaths) || !slices.Equal(g.RiskHumanFacets(), wantRiskFacets) {
		t.Errorf("unset lists = %v / %v, want the defaults", g.RiskHumanPaths(), g.RiskHumanFacets())
	}
}

// TestSettingsSetRiskLists: a JSON list written through settings lands in
// [gates] as a TOML array the gates read back; an empty list is a list; a
// value that is not a list of non-blank strings is refused and writes nothing.
// F-0516 crit. 14
func TestSettingsSetRiskLists(t *testing.T) {
	projDir, _ := settingsFixture(t, gatesBlock, "", false)
	if err := SetSetting(projDir, "gates.risk_human_paths", ScopeProject, `["src/auth/**","*.sql"]`); err != nil {
		t.Fatalf("SetSetting paths: %v", err)
	}
	if err := SetSetting(projDir, "gates.risk_human_facets", ScopeProject, `[]`); err != nil {
		t.Fatalf("SetSetting facets: %v", err)
	}
	rc := filepath.Join(projDir, ".cernerc")
	raw, _ := os.ReadFile(rc)
	if !strings.Contains(string(raw), `risk_human_paths = ["src/auth/**", "*.sql"]`) || !strings.Contains(string(raw), "risk_human_facets = []") {
		t.Errorf(".cernerc lacks the lists:\n%s", raw)
	}
	cfg, _, err := LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Gates.RiskHumanPaths(); !slices.Equal(got, []string{"src/auth/**", "*.sql"}) {
		t.Errorf("RiskHumanPaths = %v", got)
	}
	if got := cfg.Gates.RiskHumanFacets(); got == nil || len(got) != 0 {
		t.Errorf("RiskHumanFacets after [] = %#v, want an empty list, not the default", got)
	}
	if s := findSetting(t, ResolveSettings(projDir), "gates.risk_human_paths"); !slices.Equal(s.Value.([]string), []string{"src/auth/**", "*.sql"}) || s.Origin != ScopeProject {
		t.Errorf("resolved paths = %v (%s)", s.Value, s.Origin)
	}
	before, _ := os.ReadFile(rc)
	for _, bad := range []string{`"go.mod"`, `[1]`, `[""]`, `["a\nb"]`, `not json`} {
		if err := SetSetting(projDir, "gates.risk_human_paths", ScopeProject, bad); !errors.Is(err, ErrInvalidSettingValue) {
			t.Errorf("SetSetting %s: err = %v, want ErrInvalidSettingValue", bad, err)
		}
	}
	if after, _ := os.ReadFile(rc); string(after) != string(before) {
		t.Errorf("a refused write changed .cernerc:\n%s", after)
	}
	if err := UnsetSetting(projDir, "gates.risk_human_paths", ScopeProject); err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := LoadConfig(projDir); !slices.Equal(cfg.Gates.RiskHumanPaths(), wantRiskPaths) {
		t.Errorf("after unset = %v, want the default", cfg.Gates.RiskHumanPaths())
	}
}

// TestSettingsSetRiskListsRefusesBackslash: a path pattern written with
// backslashes is refused by its own sentinel, and nothing is written.
// F-0516 crit. 14
func TestSettingsSetRiskListsRefusesBackslash(t *testing.T) {
	projDir, _ := settingsFixture(t, gatesBlock, "", false)
	rc := filepath.Join(projDir, ".cernerc")
	before, _ := os.ReadFile(rc)
	err := SetSetting(projDir, "gates.risk_human_paths", ScopeProject, `[".github\\workflows\\**"]`)
	if !errors.Is(err, ErrSettingBackslash) || !errors.Is(err, ErrInvalidSettingValue) {
		t.Errorf("err = %v, want ErrSettingBackslash and ErrInvalidSettingValue", err)
	}
	if after, _ := os.ReadFile(rc); string(after) != string(before) {
		t.Errorf("a refused write changed .cernerc:\n%s", after)
	}
}
