package project

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestConfigWebhookRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{
		ProjectID: "cerne",
		Backend:   "/tmp/backend",
		Webhook:   WebhookConfig{URL: "https://hooks.example/x", Format: "slack", Timeout: 5},
	}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
}

func TestConfigNoWebhookOmitsBlock(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{ProjectID: "cerne", Backend: "/tmp/backend"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".cernerc"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(data), "[webhook]") {
		t.Fatalf(".cernerc has [webhook] block with no config set:\n%s", data)
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

func TestConfigPinWriterRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{ProjectID: "cerne", Backend: "/tmp/backend", PinWriter: "ci"}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.PinWriter != "ci" {
		t.Fatalf("PinWriter = %q, want ci", got.PinWriter)
	}

	if err := SaveConfig(dir, Config{ProjectID: "x", Backend: "/b"}); err != nil {
		t.Fatalf("SaveConfig default: %v", err)
	}
	got, _, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if got.PinWriter != "" {
		t.Fatalf("PinWriter must default to empty, got %q", got.PinWriter)
	}
}

func TestConfigDeliveryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{ProjectID: "cerne", Backend: "/tmp/backend",
		Delivery: DeliveryConfig{CommitURL: "https://gitlab.example/repo/-/commit/{sha}"}}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Delivery.CommitURL != want.Delivery.CommitURL {
		t.Fatalf("Delivery.CommitURL: got %q, want %q", got.Delivery.CommitURL, want.Delivery.CommitURL)
	}
	// Absent block ⇒ zero value, and the saved TOML carries no empty [delivery] table.
	if err := SaveConfig(dir, Config{ProjectID: "x", Backend: "/b"}); err != nil {
		t.Fatalf("SaveConfig default: %v", err)
	}
	got, _, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if got.Delivery != (DeliveryConfig{}) {
		t.Fatalf("Delivery must default to zero, got %+v", got.Delivery)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".cernerc"))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(data), "[delivery]") {
		t.Fatalf("zero Delivery must not emit a [delivery] table:\n%s", data)
	}
}

func TestConfigAgentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Config{ProjectID: "cerne", Backend: "/tmp/backend",
		Agent: AgentConfig{Profile: "Você é um analista financeiro."}}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Agent.Profile != want.Agent.Profile {
		t.Fatalf("Agent.Profile: got %q, want %q", got.Agent.Profile, want.Agent.Profile)
	}
	// Absent block ⇒ zero value, and the saved TOML carries no empty [agent] table.
	if err := SaveConfig(dir, Config{ProjectID: "x", Backend: "/b"}); err != nil {
		t.Fatalf("SaveConfig default: %v", err)
	}
	got, _, err = LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig default: %v", err)
	}
	if got.Agent != (AgentConfig{}) {
		t.Fatalf("Agent must default to zero, got %+v", got.Agent)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".cernerc"))
	if err != nil {
		t.Fatalf("read .cernerc: %v", err)
	}
	if strings.Contains(string(data), "[agent]") {
		t.Fatalf("zero Agent must not emit an [agent] table:\n%s", data)
	}
}

func TestAgentConfigResolve(t *testing.T) {
	projDir := t.TempDir()

	// Absent → empty persona (the caller falls back to the default neutral one).
	if got, err := (AgentConfig{}).Resolve(projDir); err != nil || got != "" {
		t.Fatalf("Resolve absent = %q, %v; want \"\", nil", got, err)
	}

	// Inline profile → returned verbatim.
	inline := AgentConfig{Profile: "Você é um analista financeiro."}
	if got, err := inline.Resolve(projDir); err != nil || got != inline.Profile {
		t.Fatalf("Resolve inline = %q, %v; want %q, nil", got, err, inline.Profile)
	}

	// profile_file → read relative to projDir.
	if err := os.WriteFile(filepath.Join(projDir, "persona.md"), []byte("Persona do arquivo.\n"), 0o644); err != nil {
		t.Fatalf("write persona.md: %v", err)
	}
	file := AgentConfig{ProfileFile: "persona.md"}
	got, err := file.Resolve(projDir)
	if err != nil {
		t.Fatalf("Resolve profile_file: %v", err)
	}
	if got != "Persona do arquivo.\n" {
		t.Fatalf("Resolve profile_file = %q, want file content", got)
	}

	// profile_file pointing at a missing file → error, not a silent empty persona.
	missing := AgentConfig{ProfileFile: "nope.md"}
	if _, err := missing.Resolve(projDir); err == nil {
		t.Fatalf("Resolve missing profile_file: want error, got nil")
	}

	// Both set → profile_file wins (the versioned, reviewable form).
	both := AgentConfig{Profile: "inline", ProfileFile: "persona.md"}
	if got, err := both.Resolve(projDir); err != nil || got != "Persona do arquivo.\n" {
		t.Fatalf("Resolve both = %q, %v; want file content wins", got, err)
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

// TestAgentChatTimeout pins the [agent] chat_timeout resolution (F-0224): zero
// means the 300s default (agentic chat chains queries, one-shot budgets are
// too tight), a set value wins, and the field round-trips through .cernerc.
func TestAgentChatTimeout(t *testing.T) {
	if got := (AgentConfig{}).ChatTimeoutOrDefault(); got != 300*time.Second {
		t.Fatalf("default ChatTimeout = %v, want 300s", got)
	}
	if got := (AgentConfig{ChatTimeout: 45}).ChatTimeoutOrDefault(); got != 45*time.Second {
		t.Fatalf("set ChatTimeout = %v, want 45s", got)
	}

	dir := t.TempDir()
	want := Config{ProjectID: "cerne", Backend: "/tmp/backend",
		Agent: AgentConfig{ChatTimeout: 120}}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Agent.ChatTimeout != 120 {
		t.Fatalf("ChatTimeout round-trip: got %d, want 120", got.Agent.ChatTimeout)
	}
}

// TestGuardConfig_FailMode (F-0225 crit. 5): absent/anything-but-"warn" fails
// the commit-msg guard; "warn" downgrades it to a non-blocking notice.
func TestGuardConfig_FailMode(t *testing.T) {
	for _, v := range []string{"", "fail", "bogus"} {
		g := GuardConfig{Mode: v}
		if !g.FailMode() {
			t.Errorf("Mode %q: want fail mode", v)
		}
	}
	if (GuardConfig{Mode: "warn"}).FailMode() {
		t.Fatal(`Mode "warn": want non-fail (warn) mode`)
	}
}

func TestConfigGuardModeRoundtrip(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{ProjectID: "demo", Backend: "x"}
	cfg.Guard.Mode = "warn"
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, _, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.Guard.FailMode() {
		t.Fatal("guard.mode = warn: want non-fail mode after roundtrip")
	}
}
