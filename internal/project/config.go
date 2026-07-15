// Package project handles the per-project files cerne keeps in the user's
// repository (.cernerc, docs.lock) and the local cache layout.
package project

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// ErrNoProject is the sentinel for a LoadConfig walk that reaches the
// filesystem root without finding a .cernerc.
var ErrNoProject = errors.New("no cerne project")

const configFilename = ".cernerc"

// Config is the persisted form of .cernerc (TOML, committed by the user).
type Config struct {
	ProjectID      string `toml:"project_id"`                // slug, e.g. "cerne"
	Backend        string `toml:"backend"`                   // git URL or local path
	DefaultScope   string `toml:"default_scope"`             // optional module filter
	OKF            bool   `toml:"okf,omitempty"`             // emit OKF-compatible frontmatter in .docs/ (cerne pull); flag --okf overrides
	SyncSkills     bool   `toml:"sync_skills,omitempty"`     // F-0178: cerne pull also reconciles golden-path entities into .claude/skills/learned-*; flag --sync-skills overrides
	AutocommitLock bool   `toml:"autocommit_lock,omitempty"` // F-0060: cerne commits the project-side docs.lock after a backend mutation (opt-in, default off)
	PinBranch      string `toml:"pin_branch,omitempty"`      // F-0102: docs.lock is written/committed only on this branch; empty = every branch (pre-0102 behavior)
	PushPin        bool   `toml:"push_pin,omitempty"`        // F-0180: publish the docs.lock pin to origin right after the deferred commit, when only pin commits are ahead (opt-in, default off)
	CompileOnPush  bool   `toml:"compile_on_push,omitempty"` // F-0117: a successful push recompiles the dirty wiki pages (callable [llm] mode only; opt-in, default off)
	// UnregisteredSourceIgnore lists URL substrings the unregistered-source lint
	// rule skips (F-0107) — e.g. the project's own code repo host, which is not an
	// external source. Internal/local hosts (.local, localhost, private IPs) are
	// always skipped and need no config.
	UnregisteredSourceIgnore []string `toml:"unregistered_source_ignore,omitempty"`
	// ModulePaths maps a module name to the slash-separated path globs (of the
	// user's project repo) that make up its code, e.g. core = ["internal/domain/**"].
	// The drift-suspect lint rule (F-0113) uses it to map a changed file to the
	// module(s) it belongs to. Absent (nil) makes the rule inert — opt-in.
	ModulePaths map[string][]string `toml:"module_paths,omitempty"`
	// ComponentPaths is the second, additive drift axis (F-0116): component tag →
	// path globs, matching a feature through its Components. It is what makes the
	// drift rule work in a moduleless project. Same opt-in stance as ModulePaths.
	ComponentPaths map[string][]string `toml:"component_paths,omitempty"`
	// StaleReadyDays is the window (days) an implemented ("Ready") feature may
	// wait for human acceptance before the stale-ready lint flags it (F-0131).
	// 0 (absent) means the 14-day default — read it via StaleReadyWindowDays.
	StaleReadyDays int `toml:"stale_ready_days,omitzero"`
	// StaleWorkHours is the window (hours) an active work (agent activity,
	// F-0130) may go without a done/abandon before the stale-work lint flags
	// it. 0 (absent) means the 8-hour default — read it via
	// StaleWorkWindowHours.
	StaleWorkHours int            `toml:"stale_work_hours,omitzero"`
	Gates          GatesConfig     `toml:"gates"`     // F-0131: who may cross the human gates
	Activity       ActivityConfig  `toml:"activity"`  // F-0146: auto-registered activity on feature transitions
	LLM            LLMConfig       `toml:"llm"`       // optional v1.5 LLM layer config
	Telemetry      TelemetryConfig `toml:"telemetry"` // F-0149: opt-in trace export + log level
	Delivery       DeliveryConfig  `toml:"delivery"`  // F-0179: delivery link capture (commit/MR on complete)
}

// DeliveryConfig is the optional [delivery] block of .cernerc (F-0179).
// CommitURL is an explicit template with a {sha} placeholder (e.g.
// "https://gitlab.com/rockt/cerne/-/commit/{sha}") used to resolve a bare
// commit SHA into a clickable URL — no forge heuristics: absent means a bare
// SHA is refused and only full URLs are accepted.
type DeliveryConfig struct {
	CommitURL string `toml:"commit_url,omitempty"`
}

// TelemetryConfig is the optional [telemetry] block of .cernerc (F-0149).
// Everything defaults to off/local: traces are exported only when
// OtlpEndpoint is set (OTEL_EXPORTER_OTLP_ENDPOINT overrides at runtime).
type TelemetryConfig struct {
	OtlpEndpoint string `toml:"otlp_endpoint,omitempty"` // OTLP/HTTP traces target (e.g. Tempo); empty = tracing off
	ServiceName  string `toml:"service_name,omitempty"`  // resource service.name; default "cerne"
	LogLevel     string `toml:"log_level,omitempty"`     // slog level for the server: debug|info|warn|error; default info
}

// ActivityConfig is the optional [activity] block of .cernerc (F-0146).
// Auto is a pointer so "absent" (nil → enabled, the default) is distinguishable
// from an explicit `auto = false` opt-out.
type ActivityConfig struct {
	Auto *bool `toml:"auto"`
}

// AutoEnabled resolves the auto-registration switch: on unless explicitly off.
func (a ActivityConfig) AutoEnabled() bool {
	return a.Auto == nil || *a.Auto
}

// Gate values for the [gates] block (F-0131). Default is human on both ends;
// "auto" is the explicit per-project bypass. Any other value reads as human
// (fail-safe: a typo must not silently open a gate).
const (
	GateHuman = "human"
	GateAuto  = "auto"
)

// GatesConfig is the optional [gates] block of .cernerc (F-0131): approve is
// the start-of-flow gate (spec approval), accept the end-of-flow gate (human
// validation of an implemented feature). WorkGate (F-0151) is the edit gate:
// when "on", the installed PreToolUse hook denies Edit/Write in a Claude Code
// session with no live work/feature binding. Opt-in by design — any other
// value (including absent) keeps it off, so existing projects never lock up
// on upgrade. WorkGateAllow extends the built-in allowlist (.docs/**,
// .claude/**, paths outside the project) with project-specific globs.
type GatesConfig struct {
	Approve       string   `toml:"approve,omitempty"`         // "human" (default) | "auto"
	Accept        string   `toml:"accept,omitempty"`          // "human" (default) | "auto"
	Revert        string   `toml:"revert,omitempty"`          // "human" (default) | "auto" — F-0169: undo of an approval
	WorkGate      string   `toml:"work_gate,omitempty"`       // "on" | anything else = off (default)
	WorkGateAllow []string `toml:"work_gate_allow,omitempty"` // extra allowlist globs for the gate
	WorkGateHint  string   `toml:"work_gate_hint,omitempty"`  // project-specific suffix appended to the gate's deny reason (F-0170)
	CommitTrailer string   `toml:"commit_trailer,omitempty"`  // "on" | anything else = off — prepare-commit-msg Cerne: trailer (F-0173)
}

// WorkGateEnabled resolves the edit gate: on only when explicitly configured.
func (g GatesConfig) WorkGateEnabled() bool {
	return g.WorkGate == "on"
}

// CommitTrailerEnabled resolves the commit trailer stamp (F-0173): on only
// when explicitly configured — existing projects never gain git hooks on
// upgrade.
func (g GatesConfig) CommitTrailerEnabled() bool {
	return g.CommitTrailer == "on"
}

// isZero reports whether the block carries nothing worth persisting (the
// struct holds a slice, so it is not ==-comparable).
func (g GatesConfig) isZero() bool {
	return g.Approve == "" && g.Accept == "" && g.Revert == "" && g.WorkGate == "" && len(g.WorkGateAllow) == 0 && g.WorkGateHint == "" && g.CommitTrailer == ""
}

// ApproveGate resolves the start gate: auto only when explicitly configured.
func (g GatesConfig) ApproveGate() string {
	if g.Approve == GateAuto {
		return GateAuto
	}
	return GateHuman
}

// AcceptGate resolves the end gate: auto only when explicitly configured.
func (g GatesConfig) AcceptGate() string {
	if g.Accept == GateAuto {
		return GateAuto
	}
	return GateHuman
}

// RevertGate resolves the approval-undo gate (F-0169): undoing a human act is
// a human act, so auto only when explicitly configured.
func (g GatesConfig) RevertGate() string {
	if g.Revert == GateAuto {
		return GateAuto
	}
	return GateHuman
}

// StaleReadyWindowDays returns the configured stale_ready_days or the 14-day
// default when absent/zero.
func (c Config) StaleReadyWindowDays() int {
	if c.StaleReadyDays > 0 {
		return c.StaleReadyDays
	}
	return 14
}

// StaleWorkWindowHours returns the configured stale_work_hours or the 8-hour
// default when absent/zero (F-0130).
func (c Config) StaleWorkWindowHours() int {
	if c.StaleWorkHours > 0 {
		return c.StaleWorkHours
	}
	return 8
}

// LLMConfig is the optional [llm] block of .cernerc. A secret never lives
// here: in byo-key mode the API key is read from the environment variable
// named by KeyEnv — only the NAME is persisted, never the value.
// Tags keep .cernerc clean when writing a partial config (e.g. just the mode,
// from the UI switcher): strings use omitempty (drops "") and the int uses
// omitzero (drops 0) — in BurntSushi/toml v1.6 omitempty does not drop a zero
// int, so the two tags are complementary. Defaults are applied on read by
// WithDefaults.
type LLMConfig struct {
	Mode      string `toml:"mode,omitempty"`       // "delegation" (default when empty) | "byo-key" | "claude-cli"
	Provider  string `toml:"provider,omitempty"`   // byo-key provider; default "anthropic"
	Model     string `toml:"model,omitempty"`      // model id; default "claude-opus-4-8"
	KeyEnv    string `toml:"key_env,omitempty"`    // env var NAME holding the key; default "CERNE_LLM_API_KEY"
	BaseURL   string `toml:"base_url,omitempty"`   // API base URL; default "https://api.anthropic.com"
	MaxTokens int    `toml:"max_tokens,omitzero"`  // max output tokens; default 8000
	ClaudeBin string `toml:"claude_bin,omitempty"` // claude-cli binary path; default "claude"
}

// LLM defaults applied by withDefaults when a byo-key field is empty/zero.
const (
	defaultLLMProvider  = "anthropic"
	defaultLLMModel     = "claude-opus-4-8"
	defaultLLMKeyEnv    = "CERNE_LLM_API_KEY"
	defaultLLMBaseURL   = "https://api.anthropic.com"
	defaultLLMMaxTokens = 8000
	defaultLLMClaudeBin = "claude"
)

// WithDefaults returns a copy with each empty/zero byo-key field filled by its
// default. Mode is left untouched (llm.ResolveMode owns that fallback). It is
// exported so the llm package can normalize a config before building a provider.
func (c LLMConfig) WithDefaults() LLMConfig {
	if c.Provider == "" {
		c.Provider = defaultLLMProvider
	}
	if c.Model == "" {
		c.Model = defaultLLMModel
	}
	if c.KeyEnv == "" {
		c.KeyEnv = defaultLLMKeyEnv
	}
	if c.BaseURL == "" {
		c.BaseURL = defaultLLMBaseURL
	}
	if c.MaxTokens == 0 {
		c.MaxTokens = defaultLLMMaxTokens
	}
	if c.ClaudeBin == "" {
		c.ClaudeBin = defaultLLMClaudeBin
	}
	return c
}

// SaveConfig writes <dir>/.cernerc as TOML. The [llm] block is omitted when it
// is the zero value, so a fresh project doesn't carry an empty table the user
// would collide with when adding their own [llm] config.
func SaveConfig(dir string, c Config) error {
	type wire struct {
		ProjectID                string              `toml:"project_id"`
		Backend                  string              `toml:"backend"`
		DefaultScope             string              `toml:"default_scope"`
		OKF                      bool                `toml:"okf,omitempty"`
		SyncSkills               bool                `toml:"sync_skills,omitempty"`
		AutocommitLock           bool                `toml:"autocommit_lock,omitempty"`
		PinBranch                string              `toml:"pin_branch,omitempty"`
		PushPin                  bool                `toml:"push_pin,omitempty"`
		CompileOnPush            bool                `toml:"compile_on_push,omitempty"`
		UnregisteredSourceIgnore []string            `toml:"unregistered_source_ignore,omitempty"`
		ModulePaths              map[string][]string `toml:"module_paths,omitempty"`
		ComponentPaths           map[string][]string `toml:"component_paths,omitempty"`
		StaleReadyDays           int                 `toml:"stale_ready_days,omitzero"`
		StaleWorkHours           int                 `toml:"stale_work_hours,omitzero"`
		Gates                    *GatesConfig        `toml:"gates,omitempty"`
		Activity                 *ActivityConfig     `toml:"activity,omitempty"`
		LLM                      *LLMConfig          `toml:"llm,omitempty"`
		Telemetry                *TelemetryConfig    `toml:"telemetry,omitempty"`
		Delivery                 *DeliveryConfig     `toml:"delivery,omitempty"`
	}
	w := wire{ProjectID: c.ProjectID, Backend: c.Backend, DefaultScope: c.DefaultScope, OKF: c.OKF, SyncSkills: c.SyncSkills, AutocommitLock: c.AutocommitLock, PinBranch: c.PinBranch, PushPin: c.PushPin, CompileOnPush: c.CompileOnPush, UnregisteredSourceIgnore: c.UnregisteredSourceIgnore, ModulePaths: c.ModulePaths, ComponentPaths: c.ComponentPaths, StaleReadyDays: c.StaleReadyDays, StaleWorkHours: c.StaleWorkHours}
	if !c.Gates.isZero() {
		gates := c.Gates
		w.Gates = &gates
	}
	if c.Activity != (ActivityConfig{}) {
		activity := c.Activity
		w.Activity = &activity
	}
	if c.LLM != (LLMConfig{}) {
		llm := c.LLM
		w.LLM = &llm
	}
	if c.Telemetry != (TelemetryConfig{}) {
		tel := c.Telemetry
		w.Telemetry = &tel
	}
	if c.Delivery != (DeliveryConfig{}) {
		del := c.Delivery
		w.Delivery = &del
	}
	data, err := toml.Marshal(w)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, configFilename, data)
}

// LoadConfig walks up from startDir until a .cernerc is found and returns
// the decoded config plus the directory containing it. ErrNoProject when no
// .cernerc exists up to the filesystem root.
func LoadConfig(startDir string) (Config, string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return Config{}, "", err
	}
	for {
		path := filepath.Join(dir, configFilename)
		data, err := os.ReadFile(path)
		if err == nil {
			var c Config
			if err := toml.Unmarshal(data, &c); err != nil {
				return Config{}, "", fmt.Errorf("%s: %w", path, err)
			}
			return c, dir, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return Config{}, "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Config{}, "", fmt.Errorf("%w: no %s from %s up to filesystem root", ErrNoProject, configFilename, startDir)
		}
		dir = parent
	}
}

// writeFileAtomic writes data to <dir>/<filename> atomically: temp file in
// the same dir, then rename.
func writeFileAtomic(dir, filename string, data []byte) error {
	tmp, err := os.CreateTemp(dir, filename+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, filename)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
