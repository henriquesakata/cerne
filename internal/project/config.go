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
	AutocommitLock bool   `toml:"autocommit_lock,omitempty"` // F-0060: cerne commits the project-side docs.lock after a backend mutation (opt-in, default off)
	PinBranch      string `toml:"pin_branch,omitempty"`      // F-0102: docs.lock is written/committed only on this branch; empty = every branch (pre-0102 behavior)
	CompileOnPush  bool   `toml:"compile_on_push,omitempty"` // F-0117: a successful push recompiles the dirty wiki pages (callable [llm] mode only; opt-in, default off)
	// UnregisteredSourceIgnore lists URL substrings the unregistered-source lint
	// rule skips (F-0107) — e.g. the project's own code repo host, which is not an
	// external source. Internal/local hosts (.local, localhost, private IPs) are
	// always skipped and need no config.
	UnregisteredSourceIgnore []string  `toml:"unregistered_source_ignore,omitempty"`
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
	StaleReadyDays int         `toml:"stale_ready_days,omitzero"`
	Gates          GatesConfig `toml:"gates"` // F-0131: who may cross the human gates
	LLM            LLMConfig   `toml:"llm"`   // optional v1.5 LLM layer config
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
// validation of an implemented feature).
type GatesConfig struct {
	Approve string `toml:"approve,omitempty"` // "human" (default) | "auto"
	Accept  string `toml:"accept,omitempty"`  // "human" (default) | "auto"
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

// StaleReadyWindowDays returns the configured stale_ready_days or the 14-day
// default when absent/zero.
func (c Config) StaleReadyWindowDays() int {
	if c.StaleReadyDays > 0 {
		return c.StaleReadyDays
	}
	return 14
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
		ProjectID                string     `toml:"project_id"`
		Backend                  string     `toml:"backend"`
		DefaultScope             string     `toml:"default_scope"`
		OKF                      bool       `toml:"okf,omitempty"`
		AutocommitLock           bool       `toml:"autocommit_lock,omitempty"`
		PinBranch                string              `toml:"pin_branch,omitempty"`
		CompileOnPush            bool                `toml:"compile_on_push,omitempty"`
		UnregisteredSourceIgnore []string            `toml:"unregistered_source_ignore,omitempty"`
		ModulePaths              map[string][]string `toml:"module_paths,omitempty"`
		ComponentPaths           map[string][]string `toml:"component_paths,omitempty"`
		StaleReadyDays           int                 `toml:"stale_ready_days,omitzero"`
		Gates                    *GatesConfig        `toml:"gates,omitempty"`
		LLM                      *LLMConfig          `toml:"llm,omitempty"`
	}
	w := wire{ProjectID: c.ProjectID, Backend: c.Backend, DefaultScope: c.DefaultScope, OKF: c.OKF, AutocommitLock: c.AutocommitLock, PinBranch: c.PinBranch, CompileOnPush: c.CompileOnPush, UnregisteredSourceIgnore: c.UnregisteredSourceIgnore, ModulePaths: c.ModulePaths, ComponentPaths: c.ComponentPaths, StaleReadyDays: c.StaleReadyDays}
	if c.Gates != (GatesConfig{}) {
		gates := c.Gates
		w.Gates = &gates
	}
	if c.LLM != (LLMConfig{}) {
		llm := c.LLM
		w.LLM = &llm
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
