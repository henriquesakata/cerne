// Package project handles the per-project files cerne keeps in the user's
// repository (.cernerc, docs.lock) and the local cache layout.
package project

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/henriquesakata/cerne/internal/models"
)

// ErrNoProject is the sentinel for a LoadConfig walk that reaches the
// filesystem root without finding a .cernerc.
var ErrNoProject = errors.New("no cerne project")

// ErrConfigParse: a config file (.cernerc, settings.toml) is not valid TOML,
// or does not decode into Config. Wrapped with the file's path.
var ErrConfigParse = errors.New("cannot parse config file")

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
	PinWriter      string `toml:"pin_writer,omitempty"`      // F-0198: "ci" reserves committed docs.lock writes for the explicit CI writer
	CompileOnPush  bool   `toml:"compile_on_push,omitempty"` // F-0117: a successful push recompiles the dirty wiki pages (callable [llm] mode only; opt-in, default off); ignored while wiki.enabled is off (F-0316)
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
	StaleWorkHours int `toml:"stale_work_hours,omitzero"`
	// StaleDecisionDays is the top-level form of lint.stale_decision_days
	// (F-0385), kept here so SaveConfig preserves it; read the effective
	// value via StaleDecisionDays(projDir), where [lint] wins.
	StaleDecisionDays int `toml:"stale_decision_days,omitzero"`
	// RecentWorkDays is the activity window (days) the CONTEXT.md "In progress"
	// board uses: older active works fold into one triage line (F-0319).
	// 0 (absent) means the 14-day default — read it via RecentWorkWindowDays.
	RecentWorkDays int             `toml:"recent_work_days,omitzero"`
	Gates          GatesConfig     `toml:"gates"`     // F-0131: who may cross the human gates
	Activity       ActivityConfig  `toml:"activity"`  // F-0146: auto-registered activity on feature transitions
	LLM            LLMConfig       `toml:"llm"`       // optional v1.5 LLM layer config
	Telemetry      TelemetryConfig `toml:"telemetry"` // F-0149: opt-in trace export + log level
	Delivery       DeliveryConfig  `toml:"delivery"`  // F-0179: delivery link capture (commit/MR on complete)
	Webhook        WebhookConfig   `toml:"webhook"`   // F-0206: HTTP POST on every entity state transition
	Agent          AgentConfig     `toml:"agent"`     // F-0210: domain persona for the Backlog chat
	Guard          GuardConfig     `toml:"guard"`     // F-0225: commit-msg guard severity
}

// AgentConfig is the optional [agent] block of .cernerc (F-0210): the domain
// persona the Backlog chat assumes (e.g. a finance analyst on a finance
// project). Profile is inline text; ProfileFile is a path, relative to the
// project root, to a committable persona doc — when both are set, ProfileFile
// wins (the versioned, reviewable form). Absent means a neutral
// requirements-analyst persona (see app.DefaultAgentPersona).
type AgentConfig struct {
	Profile     string `toml:"profile,omitempty"`
	ProfileFile string `toml:"profile_file,omitempty"`
	ChatTimeout int    `toml:"chat_timeout,omitzero"` // F-0224: chat call budget in seconds; default 300 (agentic chains cost more than one-shot)
	// RetrievalBudget caps, in characters, the backend context injected into
	// each chat turn (F-0253). It lives here, next to chat_timeout, because
	// [agent] is already the Backlog chat's config block.
	RetrievalBudget int `toml:"retrieval_budget,omitzero"`
	// PlanRoastTimeout is the per-critic budget of the plan roast in seconds
	// (F-0275): spec + plan double the input, so the spec roast's budget does
	// not carry over. Twice ChatTimeoutOrDefault is both the default AND the
	// floor — configuring less than that is raised, per the spec's ">= 2x".
	PlanRoastTimeout int `toml:"plan_roast_timeout,omitzero"`
}

// RetrievalBudgetOrDefault returns the per-turn retrieval budget in
// characters: the configured retrieval_budget, or 6000 when absent/zero —
// roughly 1.5k tokens of grounding, small beside a chat prompt's ceiling.
// A negative value turns the injection off (app.BuildTurnContext treats a
// non-positive budget as disabled).
func (a AgentConfig) RetrievalBudgetOrDefault() int {
	if a.RetrievalBudget != 0 {
		return a.RetrievalBudget
	}
	return 6000
}

// ChatTimeoutOrDefault returns the chat call budget: the configured
// chat_timeout, or 300s when absent/zero — sized for the agentic claude-cli
// mode (F-0224), where the agent chains cerne read commands before answering.
func (a AgentConfig) ChatTimeoutOrDefault() time.Duration {
	if a.ChatTimeout > 0 {
		return time.Duration(a.ChatTimeout) * time.Second
	}
	return 300 * time.Second
}

// PlanRoastTimeoutOrDefault returns the plan roast's per-critic budget
// (F-0275 criterion 3). Two documents double a critic's input, so the floor
// is twice the spec roast's budget: a configured value below it is raised
// rather than silently starving every round into a partial report.
func (a AgentConfig) PlanRoastTimeoutOrDefault() time.Duration {
	floor := 2 * a.ChatTimeoutOrDefault()
	if d := time.Duration(a.PlanRoastTimeout) * time.Second; d > floor {
		return d
	}
	return floor
}

// Resolve returns the persona text: ProfileFile's content (read relative to
// projDir) when set, else Profile verbatim, else "" (the caller falls back to
// the default persona). A ProfileFile that cannot be read is a real config
// error, not a silent empty persona.
func (a AgentConfig) Resolve(projDir string) (string, error) {
	if a.ProfileFile != "" {
		data, err := os.ReadFile(filepath.Join(projDir, a.ProfileFile))
		if err != nil {
			return "", fmt.Errorf("[agent] profile_file: %w", err)
		}
		return string(data), nil
	}
	return a.Profile, nil
}

// WebhookConfig is the optional [webhook] block of .cernerc (F-0206). Absent
// (URL empty) means silence: no request is ever sent. Format selects the
// payload shape; Timeout (seconds) bounds the POST so a dead endpoint never
// stalls a transition — both default when zero, resolved by the notify
// package, not here (this struct stays a plain TOML mirror).
type WebhookConfig struct {
	URL     string `toml:"url,omitempty"`
	Format  string `toml:"format,omitempty"` // "json" (default) | "slack"
	Timeout int    `toml:"timeout,omitzero"` // seconds; default 3
}

// DeliveryConfig is the optional [delivery] block of .cernerc (F-0179).
// CommitURL is an explicit template with a {sha} placeholder (e.g.
// "https://github.com/henriquesakata/cerne/commit/{sha}") used to resolve a bare
// commit SHA into a clickable URL — no forge heuristics: absent means a bare
// SHA is refused and only full URLs are accepted.
//
// Forge (F-0355) names the forge that hosts the delivery repository —
// "github", "gitlab" or "gitea" — and turns on the PR lookup on complete and
// the default-branch check on accept; host, owner and repo come from CommitURL.
// PRURL is a template with {n} that resolves `--mr #<n>` / `!<n>`.
type DeliveryConfig struct {
	CommitURL string `toml:"commit_url,omitempty"`
	Forge     string `toml:"forge,omitempty"`
	PRURL     string `toml:"pr_url,omitempty"`
}

// ErrDeliveryForge is the sentinel for an unsupported [delivery] forge.
var ErrDeliveryForge = errors.New("unsupported [delivery] forge")

// DeliveryForges are the supported [delivery] forge values (F-0355).
var DeliveryForges = []string{"github", "gitlab", "gitea"}

func (d DeliveryConfig) validate() error {
	if d.Forge == "" || slices.Contains(DeliveryForges, d.Forge) {
		return nil
	}
	return fmt.Errorf("%w: unknown [delivery] forge %q (supported: %s)", ErrDeliveryForge, d.Forge, strings.Join(DeliveryForges, ", "))
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
	Gauntlet      string   `toml:"gauntlet,omitempty"`        // "off" = light mode (F-0364) | anything else = on (default)
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

// GauntletEnabled resolves the Gauntlet (F-0364): on unless the literal "off"
// — the bar at approve and the Gauntlet record at complete are charged. Like
// approve/accept, a typo ("of", "OFF", "false") never opens a gate.
func (g GatesConfig) GauntletEnabled() bool {
	return g.Gauntlet != "off"
}

// isZero reports whether the block carries nothing worth persisting (the
// struct holds a slice, so it is not ==-comparable).
func (g GatesConfig) isZero() bool {
	return g.Approve == "" && g.Accept == "" && g.Revert == "" && g.WorkGate == "" && len(g.WorkGateAllow) == 0 && g.WorkGateHint == "" && g.CommitTrailer == "" && g.Gauntlet == ""
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

// GuardConfig is the optional [guard] block of .cernerc (F-0225): the
// severity of the commit-msg guard when a cited F-/T-/E- id is still
// candidate/approved (or closed: rejected/superseded). Default is fail; the
// project opts into a non-blocking notice with mode = "warn".
type GuardConfig struct {
	Mode string `toml:"mode,omitempty"` // "fail" (default) | "warn"
}

// FailMode resolves the guard severity: fail unless explicitly set to "warn".
func (g GuardConfig) FailMode() bool {
	return g.Mode != "warn"
}

// StaleReadyWindowDays returns the configured stale_ready_days or the 14-day
// default when absent/zero.
func (c Config) StaleReadyWindowDays() int {
	if c.StaleReadyDays > 0 {
		return c.StaleReadyDays
	}
	return 14
}

// RecentWorkWindowDays returns the configured recent_work_days or the 14-day
// default when absent/zero (F-0319).
func (c Config) RecentWorkWindowDays() int {
	if c.RecentWorkDays > 0 {
		return c.RecentWorkDays
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
	Model     string `toml:"model,omitempty"`      // model id; default: the model catalog's default (F-0311)
	KeyEnv    string `toml:"key_env,omitempty"`    // env var NAME holding the key; default "CERNE_LLM_API_KEY"
	BaseURL   string `toml:"base_url,omitempty"`   // API base URL; default "https://api.anthropic.com"
	MaxTokens int    `toml:"max_tokens,omitzero"`  // max output tokens; default 8000
	ClaudeBin string `toml:"claude_bin,omitempty"` // claude-cli binary path; default "claude"
	// NoFunctionCalling declares a byo-key endpoint without tool use (an
	// OpenAI-compatible gateway or model that ignores or rejects tools): the
	// chat then has no execution channel and degrades honestly (F-0323 C7).
	NoFunctionCalling bool `toml:"no_function_calling,omitempty"`
}

// LLM defaults applied by withDefaults when a byo-key field is empty/zero.
const (
	defaultLLMProvider  = "anthropic"
	defaultLLMKeyEnv    = "CERNE_LLM_API_KEY"
	defaultLLMBaseURL   = "https://api.anthropic.com"
	defaultLLMMaxTokens = 8000
	defaultLLMClaudeBin = "claude"
)

// defaultModel is the resolved model catalog's default — the global file over
// the embedded baseline (F-0311 criteria 1/2) — so a hand-set default_model
// reaches every LLM call, not only the UI.
func defaultModel() string {
	dir, err := ConfigDir()
	if err != nil {
		return models.Baseline().DefaultModel
	}
	cat, _ := models.Load(dir)
	return cat.DefaultModel
}

// llmProviderOpenAI is the OpenAI-compatible byo-key provider (F-0323).
const llmProviderOpenAI = "openai"

// WithDefaults returns a copy with each empty/zero byo-key field filled by its
// default. Mode is left untouched (llm.ResolveMode owns that fallback). It is
// exported so the llm package can normalize a config before building a provider.
func (c LLMConfig) WithDefaults() LLMConfig {
	if c.Provider == "" {
		c.Provider = defaultLLMProvider
	}
	// F-0323: the openai provider gets no Anthropic default model (it would
	// never exist there) and its own base URL.
	if c.Model == "" && c.Provider != llmProviderOpenAI {
		c.Model = defaultModel()
	}
	if c.KeyEnv == "" {
		c.KeyEnv = defaultLLMKeyEnv
	}
	if c.BaseURL == "" && c.Provider == llmProviderOpenAI {
		c.BaseURL = "https://api.openai.com"
	} else if c.BaseURL == "" {
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
// would collide with when adding their own [llm] config. It writes a whole
// file; edit an existing one via SetSetting/SetLLMMode.
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
		PinWriter                string              `toml:"pin_writer,omitempty"`
		CompileOnPush            bool                `toml:"compile_on_push,omitempty"`
		UnregisteredSourceIgnore []string            `toml:"unregistered_source_ignore,omitempty"`
		ModulePaths              map[string][]string `toml:"module_paths,omitempty"`
		ComponentPaths           map[string][]string `toml:"component_paths,omitempty"`
		StaleReadyDays           int                 `toml:"stale_ready_days,omitzero"`
		StaleWorkHours           int                 `toml:"stale_work_hours,omitzero"`
		StaleDecisionDays        int                 `toml:"stale_decision_days,omitzero"`
		RecentWorkDays           int                 `toml:"recent_work_days,omitzero"`
		Gates                    *GatesConfig        `toml:"gates,omitempty"`
		Activity                 *ActivityConfig     `toml:"activity,omitempty"`
		LLM                      *LLMConfig          `toml:"llm,omitempty"`
		Telemetry                *TelemetryConfig    `toml:"telemetry,omitempty"`
		Delivery                 *DeliveryConfig     `toml:"delivery,omitempty"`
		Webhook                  *WebhookConfig      `toml:"webhook,omitempty"`
		Agent                    *AgentConfig        `toml:"agent,omitempty"`
		Guard                    *GuardConfig        `toml:"guard,omitempty"`
	}
	w := wire{ProjectID: c.ProjectID, Backend: c.Backend, DefaultScope: c.DefaultScope, OKF: c.OKF, SyncSkills: c.SyncSkills, AutocommitLock: c.AutocommitLock, PinBranch: c.PinBranch, PushPin: c.PushPin, PinWriter: c.PinWriter, CompileOnPush: c.CompileOnPush, UnregisteredSourceIgnore: c.UnregisteredSourceIgnore, ModulePaths: c.ModulePaths, ComponentPaths: c.ComponentPaths, StaleReadyDays: c.StaleReadyDays, StaleWorkHours: c.StaleWorkHours, StaleDecisionDays: c.StaleDecisionDays, RecentWorkDays: c.RecentWorkDays}
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
	if c.Webhook != (WebhookConfig{}) {
		hook := c.Webhook
		w.Webhook = &hook
	}
	if c.Agent != (AgentConfig{}) {
		agent := c.Agent
		w.Agent = &agent
	}
	if c.Guard != (GuardConfig{}) {
		guard := c.Guard
		w.Guard = &guard
	}
	data, err := toml.Marshal(w)
	if err != nil {
		return err
	}
	return writeFileAtomic(dir, configFilename, data)
}

// SetLLMMode sets [llm] mode in <dir>/.cernerc by editing that one line, so
// comments and tables Config does not model survive (F-0314).
func SetLLMMode(dir, mode string) error {
	_, err := editTOMLFile(filepath.Join(dir, configFilename), "llm", "mode", mode)
	return err
}

// LoadConfig walks up from startDir until a .cernerc is found and returns
// the decoded config plus the directory containing it. ErrNoProject when no
// .cernerc exists up to the filesystem root.
func LoadConfig(startDir string) (Config, string, error) {
	dir, err := findConfigDir(startDir)
	if err != nil {
		return Config{}, "", err
	}
	return readConfig(dir)
}

// configWarnings receives the config's non-fatal warnings (a var so tests
// capture them).
var configWarnings io.Writer = os.Stderr

// warnedStray dedupes the stray-key warning per file and key: the config is
// loaded many times per process (the UI server per request).
var warnedStray sync.Map

// warnStrayGatesKeys names every key decoded under [gates] that is not a
// gates key (F-0364): init ends .cernerc with [gates], so a top-level key
// appended at the end of the file lands inside the table and would otherwise
// be ignored silently. A warning, never a refusal.
func warnStrayGatesKeys(path string, md toml.MetaData) {
	for _, k := range md.Undecoded() {
		if len(k) != 2 || k[0] != "gates" {
			continue
		}
		if _, seen := warnedStray.LoadOrStore(path+"\x00"+k[1], true); seen {
			continue
		}
		fmt.Fprintf(configWarnings, "warning: %s: %q is not a [gates] key and is ignored there — move it above [gates] (top-level keys go before the first table)\n", path, k[1])
	}
}

func readConfig(dir string) (Config, string, error) {
	path := filepath.Join(dir, configFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, "", err
	}
	var c Config
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Config{}, "", fmt.Errorf("%s: %w: %w", path, ErrConfigParse, err)
	}
	warnStrayGatesKeys(path, md)
	if err := c.Delivery.validate(); err != nil {
		return Config{}, "", fmt.Errorf("%s: %w", path, err)
	}
	return c, dir, nil
}

// noProjectAtError is ErrNoProject for an explicit directory (F-0354); its text
// is the user-facing message, so it carries no sentinel prefix.
type noProjectAtError struct{ msg string }

func (e *noProjectAtError) Error() string        { return e.msg }
func (e *noProjectAtError) Is(target error) bool { return target == ErrNoProject }

// crossedGitRootError is ErrNoProject for a walk that found a .cernerc only
// above the first git root it crossed (F-0372): a clone, fixture or submodule
// living inside a cerne project is not that project unless the user says so.
type crossedGitRootError struct{ GitRoot, ProjectDir string }

func (e *crossedGitRootError) Error() string {
	return fmt.Sprintf("no cerne project in this git repository (%s); the nearest %s is at %s, outside it — use --dir \"%s\" (or set CERNE_ROOT=\"%s\") to work on that project on purpose", e.GitRoot, configFilename, e.ProjectDir, e.ProjectDir, e.ProjectDir)
}
func (e *crossedGitRootError) Is(target error) bool { return target == ErrNoProject }

// RequireConfigAt checks that <dir> holds a .cernerc, without walking up and
// without parsing it (F-0354): a directory the user named explicitly (--dir) is
// the project, never a place to start a search from. CERNE_ROOT does not apply
// either. The error names the nearest project above dir, if any, so the fix is
// one line.
func RequireConfigAt(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	_, err = os.Stat(filepath.Join(abs, configFilename))
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	msg := fmt.Sprintf("no %s in %s (--dir names the project directory; it does not search parent directories)", configFilename, abs)
	if parent := filepath.Dir(abs); parent != abs {
		if above := nearestProjectAbove(parent); above != "" {
			msg += fmt.Sprintf("\nnearest project above: %s — pass --dir %s to use it", above, above)
		}
	}
	return &noProjectAtError{msg}
}

// nearestProjectAbove is the project the single walk (walkToConfigDir) finds
// from dir, across a git boundary too: it only words a hint for a --dir the
// user named, never resolves a project to read or write. "" when none.
func nearestProjectAbove(dir string) string {
	found, err := walkToConfigDir(dir)
	var crossed *crossedGitRootError
	if errors.As(err, &crossed) {
		return crossed.ProjectDir
	}
	return found
}

// findConfigDir walks up from startDir to the directory holding a .cernerc,
// without decoding it, falling back to CERNE_ROOT when the walk finds none.
// The walk refuses a .cernerc above the first git root it crossed (F-0372);
// CERNE_ROOT, an explicit request, still wins there.
func findConfigDir(startDir string) (string, error) {
	dir, err := walkToConfigDir(startDir)
	if !errors.Is(err, ErrNoProject) {
		return dir, err
	}
	var crossed *crossedGitRootError
	if !errors.As(err, &crossed) {
		return rootFromEnv(startDir)
	}
	if os.Getenv("CERNE_ROOT") == "" {
		return "", err
	}
	// A CERNE_ROOT without .cernerc keeps the boundary refusal, which names
	// the git root and the project above.
	root, envErr := rootFromEnv(startDir)
	if errors.Is(envErr, ErrNoProject) {
		return "", err
	}
	return root, envErr
}

// FoundByWalk reports whether the upward walk from startDir alone, without
// CERNE_ROOT, resolves a project — so the CLI announces every adoption of
// CERNE_ROOT, including one at a git boundary inside the project (F-0372).
func FoundByWalk(startDir string) bool {
	_, err := walkToConfigDir(startDir)
	return err == nil
}

// walkToConfigDir walks up from startDir to the nearest .cernerc. It records
// the first git root it crosses (a .git directory or file, so worktrees and
// submodules are roots too) and a .cernerc above it is crossedGitRootError
// (F-0372). Plain ErrNoProject when none exists up to the filesystem root.
func walkToConfigDir(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}
	gitRoot := ""
	for {
		_, err := os.Stat(filepath.Join(dir, configFilename))
		if err == nil {
			if gitRoot != "" {
				return "", &crossedGitRootError{GitRoot: gitRoot, ProjectDir: dir}
			}
			return dir, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if gitRoot == "" {
			// Any .git entry marks a root, as git's own discovery treats it.
			_, err := os.Lstat(filepath.Join(dir, ".git"))
			if err == nil {
				gitRoot = dir
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNoProject
		}
		dir = parent
	}
}

// rootFromEnv is the last resort of findConfigDir (F-0876): the upward walk
// from startDir found nothing, so CERNE_ROOT may name the project directory.
// The walk keeps priority, so a stray variable never hijacks a project the
// working directory already belongs to.
func rootFromEnv(startDir string) (string, error) {
	root := os.Getenv("CERNE_ROOT")
	if root == "" {
		return "", fmt.Errorf("%w: no %s from %s up to filesystem root (set CERNE_ROOT to the project directory)", ErrNoProject, configFilename, startDir)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, configFilename)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("%w: CERNE_ROOT=%s has no %s (and none from %s up to filesystem root)", ErrNoProject, root, configFilename, startDir)
		}
		return "", err
	}
	return root, nil
}

// writeFileAtomic writes data to <dir>/<filename> atomically: temp file in
// the target's dir, then rename. A symlink is written through (its target is
// replaced, the link kept) and an existing file keeps its mode bits; a new
// one gets 0644 (F-0314, as git config does).
func writeFileAtomic(dir, filename string, data []byte) error {
	path, mode, err := writeTarget(filepath.Join(dir, filename))
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// writeTarget resolves path through symlinks, so a rewrite lands on the
// link's target, and returns the mode the rewrite keeps: the existing file's,
// or 0644 for a new one.
func writeTarget(path string) (string, os.FileMode, error) {
	real, err := filepath.EvalSymlinks(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, 0o644, nil
	}
	if err != nil {
		return "", 0, err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", 0, err
	}
	return real, fi.Mode().Perm(), nil
}
