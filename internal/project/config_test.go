package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		ProjectID:    "cerne",
		Backend:      "https://gitlab.com/rockt/cerne-docs.git",
		DefaultScope: "core",
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, foundDir, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
	if foundDir != dir {
		t.Fatalf("LoadConfig dir = %q, want %q", foundDir, dir)
	}
}

func TestConfigCompileOnPushRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{ProjectID: "cerne", Backend: "/tmp/backend", CompileOnPush: true}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.CompileOnPush {
		t.Fatalf("CompileOnPush not persisted")
	}
	// Absent in TOML ⇒ false (opt-in).
	if err := SaveConfig(dir, Config{ProjectID: "x", Backend: "/b"}); err != nil {
		t.Fatalf("SaveConfig default: %v", err)
	}
	got, _, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if got.CompileOnPush {
		t.Fatalf("CompileOnPush must default to false")
	}
}

func TestConfigModulePathsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		ProjectID: "cerne",
		Backend:   "/tmp/backend",
		ModulePaths: map[string][]string{
			"core": {"internal/domain/**", "internal/store/**"},
			"cli":  {"internal/cli/**"},
		},
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got.ModulePaths, want.ModulePaths) {
		t.Fatalf("ModulePaths = %+v, want %+v", got.ModulePaths, want.ModulePaths)
	}
}

func TestConfigComponentPathsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		ProjectID: "cerne",
		Backend:   "/tmp/backend",
		ComponentPaths: map[string][]string{
			"drift": {"internal/lint/drift.go", "internal/cli/drift.go"},
			"llm":   {"internal/llm/**"},
		},
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got.ComponentPaths, want.ComponentPaths) {
		t.Fatalf("ComponentPaths = %+v, want %+v", got.ComponentPaths, want.ComponentPaths)
	}
}

func TestLoadConfigWalksUpFromNestedDir(t *testing.T) {
	root := t.TempDir()
	want := Config{ProjectID: "nested", Backend: "/tmp/backend"}
	if err := SaveConfig(root, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	got, foundDir, err := LoadConfig(nested)
	if err != nil {
		t.Fatalf("LoadConfig from nested: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
	if foundDir != root {
		t.Fatalf("LoadConfig dir = %q, want %q", foundDir, root)
	}
}

func TestLoadConfigNoProject(t *testing.T) {
	_, _, err := LoadConfig(t.TempDir())
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("LoadConfig err = %v, want ErrNoProject", err)
	}
}

// TestConfigSyncSkillsRoundTrip pins the .cernerc `sync_skills` key (F-0178):
// it persists and reloads when true.
func TestConfigSyncSkillsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", SyncSkills: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), "sync_skills = true") {
		t.Fatalf(".cernerc missing sync_skills key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.SyncSkills {
		t.Fatalf("loaded SyncSkills = false, want true")
	}
}

// TestConfigOKFRoundTrip pins the .cernerc `okf` key: it persists and reloads
// when true, and is omitted (omitempty) when false.
func TestConfigOKFRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", OKF: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), "okf = true") {
		t.Fatalf(".cernerc missing okf key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.OKF {
		t.Fatalf("loaded OKF = false, want true")
	}

	// omitempty: a false OKF is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "okf") {
		t.Fatalf("okf=false should be omitted:\n%s", raw)
	}
}

// TestConfigPinBranchRoundTrip pins the .cernerc `pin_branch` key (F-0102):
// persists and reloads when set, omitted (omitempty) when empty — an absent key
// keeps the pre-0102 behavior (docs.lock pinned on every branch).
func TestConfigPinBranchRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", PinBranch: "main"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), `pin_branch = "main"`) {
		t.Fatalf(".cernerc missing pin_branch key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.PinBranch != "main" {
		t.Fatalf("loaded PinBranch = %q, want %q", got.PinBranch, "main")
	}

	// omitempty: an empty PinBranch is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "pin_branch") {
		t.Fatalf("empty pin_branch should be omitted:\n%s", raw)
	}
}

// TestConfigAutocommitLockRoundTrip pins the .cernerc `autocommit_lock` key
// (F-0060): persists and reloads when true, omitted (omitempty) when false.
func TestConfigAutocommitLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", AutocommitLock: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), "autocommit_lock = true") {
		t.Fatalf(".cernerc missing autocommit_lock key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.AutocommitLock {
		t.Fatalf("loaded AutocommitLock = false, want true")
	}

	// omitempty: a false AutocommitLock is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "autocommit_lock") {
		t.Fatalf("autocommit_lock=false should be omitted:\n%s", raw)
	}
}

// TestConfigPushPinRoundTrip pins the .cernerc `push_pin` key (F-0180):
// persists and reloads when true, omitted (omitempty) when false.
func TestConfigPushPinRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", PushPin: true}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), "push_pin = true") {
		t.Fatalf(".cernerc missing push_pin key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.PushPin {
		t.Fatalf("loaded PushPin = false, want true")
	}

	// omitempty: a false PushPin is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "push_pin") {
		t.Fatalf("push_pin=false should be omitted:\n%s", raw)
	}
}

// TestConfigUnregisteredSourceIgnoreRoundTrip pins the .cernerc
// `unregistered_source_ignore` key (F-0107): the list persists and reloads, and
// is omitted (omitempty) when empty.
func TestConfigUnregisteredSourceIgnoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := []string{"gitlab.com/rockt/hub", "example.internal"}
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x", UnregisteredSourceIgnore: want}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got.UnregisteredSourceIgnore, want) {
		t.Fatalf("loaded UnregisteredSourceIgnore = %v, want %v", got.UnregisteredSourceIgnore, want)
	}

	// omitempty: an empty list is not written to .cernerc.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig off: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "unregistered_source_ignore") {
		t.Fatalf("empty list should be omitted:\n%s", raw)
	}
}

// TestConfigWorkGateRoundTrip pins the .cernerc [gates] work_gate keys
// (F-0151): the gate is opt-in (enabled only on the explicit "on"), the extra
// allowlist round-trips, and an absent block stays omitted.
func TestConfigWorkGateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ProjectID: "demo", Backend: "x"}
	cfg.Gates.WorkGate = "on"
	cfg.Gates.WorkGateAllow = []string{"docs/**"}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), `work_gate = "on"`) {
		t.Fatalf(".cernerc missing work_gate key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.Gates.WorkGateEnabled() {
		t.Fatal("work_gate = on: want enabled")
	}
	if len(got.Gates.WorkGateAllow) != 1 || got.Gates.WorkGateAllow[0] != "docs/**" {
		t.Fatalf("WorkGateAllow = %v, want [docs/**]", got.Gates.WorkGateAllow)
	}

	// Fail-safe default: absent or any non-"on" value keeps the gate off.
	for _, v := range []string{"", "off", "true", "yes"} {
		g := GatesConfig{WorkGate: v}
		if g.WorkGateEnabled() {
			t.Errorf("WorkGate %q: want disabled", v)
		}
	}

	// omitempty: a default config writes no [gates] block.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig default: %v", err)
	}
	raw, err = os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(raw), "work_gate") || strings.Contains(string(raw), "[gates]") {
		t.Fatalf("default config should omit [gates]:\n%s", raw)
	}
}

// TestConfigWorkGateHintRoundTrip pins the .cernerc [gates] work_gate_hint key
// (F-0170): a project-specific suffix for the gate's deny reason round-trips,
// an absent key reads as empty, and a hint alone is enough to persist the
// [gates] block.
func TestConfigWorkGateHintRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ProjectID: "demo", Backend: "x"}
	cfg.Gates.WorkGateHint = "consult the convention catalog first"
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, configFilename))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if !strings.Contains(string(raw), `work_gate_hint = "consult the convention catalog first"`) {
		t.Fatalf(".cernerc missing work_gate_hint key:\n%s", raw)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Gates.WorkGateHint != "consult the convention catalog first" {
		t.Fatalf("WorkGateHint = %q", got.Gates.WorkGateHint)
	}

	// Absent key -> empty; default config writes no [gates] block at all.
	if err := SaveConfig(dir, Config{ProjectID: "demo", Backend: "x"}); err != nil {
		t.Fatalf("SaveConfig default: %v", err)
	}
	got, _, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if got.Gates.WorkGateHint != "" {
		t.Fatalf("default WorkGateHint = %q, want empty", got.Gates.WorkGateHint)
	}
}

// TestConfigCommitTrailerToggle (F-0173 crit. 4): the [gates] commit_trailer
// key round-trips and is opt-in — enabled only on the explicit "on".
func TestConfigCommitTrailerToggle(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ProjectID: "demo", Backend: "x"}
	cfg.Gates.CommitTrailer = "on"
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !got.Gates.CommitTrailerEnabled() {
		t.Fatal("commit_trailer = on: want enabled")
	}
	for _, v := range []string{"", "off", "true"} {
		g := GatesConfig{CommitTrailer: v}
		if g.CommitTrailerEnabled() {
			t.Errorf("CommitTrailer %q: want disabled", v)
		}
	}
}
