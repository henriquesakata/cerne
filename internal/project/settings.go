package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/henriquesakata/cerne/internal/tracker"
)

// Setting scopes, in precedence order, and the origin of a value no scope sets
// (F-0314). The project scope is the committed .cernerc; the host scope is
// settings.toml in ConfigDir, outside every repository.
const (
	ScopeProject  = "project"
	ScopeHost     = "host"
	OriginDefault = "default"
	// OriginDetected: no scope stores the value and a dynamic default found one
	// (llm.mode: the subscription CLI on PATH, F-0414).
	OriginDetected = "detected"
)

const hostSettingsFilename = "settings.toml"

// Refusals of a settings read or write (F-0314).
var (
	ErrUnknownSetting      = errors.New("unknown setting")
	ErrInvalidSettingValue = errors.New("invalid setting value")
	ErrSettingScope        = errors.New("unknown scope")
	// ErrSettingNotSet: an unset of a key the scope's file does not hold.
	ErrSettingNotSet = errors.New("nothing to unset")
	// ErrSettingBackslash: a list item with a backslash (F-0516). Path
	// patterns use forward slashes; a Windows-style one would match nothing.
	ErrSettingBackslash = errors.New("a backslash in a list item; write paths with forward slashes")
	// ErrConfigBusy: another writer held the file's lock past
	// configLockTimeout. Nothing was written.
	ErrConfigBusy = errors.New("config file is locked by another writer")
)

// configLockTimeout bounds the wait for another writer's <file>.lock.
var configLockTimeout = 2 * time.Second

// Setting is one declared configuration key. ID is "table.key", the TOML
// address in both scopes (git config's section.key, R-0043).
type Setting struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Type        string   `json:"type"`
	Default     any      `json:"default"`
	Scopes      []string `json:"scopes"`
	Description string   `json:"description"`
	// Group is the subject page the key belongs to, one of settingGroups.
	Group string `json:"group"`
	// Enum limits a string key to these values (F-0353); empty = any string.
	Enum []string `json:"enum,omitempty"`
	// TopLevel is a top-level .cernerc key read as this setting when the
	// project file has no [table] key (F-0385).
	TopLevel string `json:"-"`
	// HumanOnly marks a key only a human writes: the CLI refuses an agent
	// actor (F-0392). The server only serializes it, for the UI to guard the
	// key; the CLI still enforces it.
	HumanOnly bool `json:"human_only,omitempty"`
	// MaxLen is the longest string the key accepts, in characters; 0 = the
	// default maxSettingString (a URL needs more than a display name). The UI
	// caps its text field with it (F-0449).
	MaxLen int `json:"max_len,omitempty"`
	// DynamicDefault, when set, supplies the value of a key no scope stores:
	// "" keeps Default, anything else becomes the value with OriginDetected.
	// The registry does not know what it detects.
	DynamicDefault func() string                 `json:"-"`
	Fields         []Setting                     `json:"fields,omitempty"` // the columns; Fields[0] is the record key: non-empty, unique
	CheckRecord    func(map[string]string) error `json:"-"`                // domain check of one record, after the fields pass
}

// settingGroups are the subject groups in display order (F-0413).
var settingGroups = []string{"general", "process", "interface", "integrations", "ai"}

// SettingHumanOnly reports whether id is declared human-only.
func SettingHumanOnly(id string) bool {
	i := slices.IndexFunc(settingsRegistry, func(s Setting) bool { return s.ID == id })
	return i >= 0 && settingsRegistry[i].HumanOnly
}

// ResolvedSetting is a Setting with its effective value and the scope it came
// from: ScopeProject, ScopeHost or OriginDefault. Stored holds the valid
// value each scope that sets the key stores, keyed by scope, so a value a
// higher scope overrides stays visible; Invalid lists the stored values of
// this key the resolution skipped, in any scope.
type ResolvedSetting struct {
	Setting
	Value   any              `json:"value"`
	Origin  string           `json:"origin"`
	Stored  map[string]any   `json:"stored,omitempty"`
	Invalid []SettingProblem `json:"invalid,omitempty"`
	// Fallback is the value used when this one is empty (F-0333). Only the
	// /settings endpoint fills it, on every answer; ResolveSettings never does.
	Fallback string `json:"fallback,omitempty"`
}

// SettingProblem is what the resolution skipped instead of failing: a stored
// value outside the key's type (Raw is its TOML text) or a whole scope file it
// could not read.
type SettingProblem struct {
	Scope string `json:"scope"`
	File  string `json:"file"`
	Raw   string `json:"raw,omitempty"`
	Error string `json:"error"`
}

// SettingsView is every declared setting resolved for one project, plus the
// file behind each scope (keyed by scope; no project key outside a project)
// and the scope files that could not be read.
type SettingsView struct {
	Settings []ResolvedSetting `json:"settings"`
	Files    map[string]string `json:"files"`
	Errors   []SettingProblem  `json:"errors,omitempty"`
}

// settingsRegistry is the one place a setting is declared: the CLI, the
// endpoint and the UI all derive from it. Kept in id order.
var settingsRegistry = []Setting{
	{
		ID: "channel.human_name", Group: "general", Title: "Your name in the channel", Type: "string", Default: "",
		Scopes:      []string{ScopeHost},
		Description: "The name that signs the messages you send from the UI on a project's permanent channel. It applies to every project on this machine. It is not the project's name, which is set in the “Project name” field of the Settings screen. Empty uses your OS user name.",
	},
	{
		ID: "delivery.commit_url", Group: "integrations", Title: "Commit URL", Type: "string", Default: "", MaxLen: 256,
		Scopes:      []string{ScopeProject},
		Description: "The template that turns a bare commit SHA into a link, with a {sha} placeholder, for example https://github.com/owner/repo/commit/{sha}. Empty accepts only full commit URLs.",
	},
	{
		ID: "delivery.forge", Group: "integrations", Title: "Delivery forge", Type: "string", Default: "", Enum: DeliveryForges,
		Scopes:      []string{ScopeProject},
		Description: "The forge that hosts the delivery repository: github, gitlab or gitea. Set, complete looks up the PR that holds the delivery commit and accept checks that it reached the default branch; host, owner and repo come from the commit URL. Empty turns both checks off.",
	},
	{
		ID: "delivery.pr_url", Group: "integrations", Title: "PR URL", Type: "string", Default: "", MaxLen: 256,
		Scopes:      []string{ScopeProject},
		Description: "The template that turns --mr #<n> into a link, with an {n} placeholder, for example https://github.com/owner/repo/pull/{n}.",
	},
	{
		// F-0515. A new accept gate is one more value here and in AcceptGate.
		ID: "gates.accept", Group: "process", Title: "Delivery acceptance", Type: "string", Default: GateHuman, Enum: []string{GateHuman, GateMerge, GateAuto, GateRisk},
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "Who accepts a delivered card. A person (human): complete stops at Ready and a person accepts the card; an agent is refused. On the PR merge (merge): the merge of the delivery PR is the acceptance, recorded by cerne feature accept --merged; an accept typed by hand stays a person's. Automatic (auto): complete carries the card straight to done, marked accepted without human review. By risk (risk): complete accepts the card on its own only when all three hold: the delivery touches no sensitive path or facet (the two lists below), its Gauntlet ended in a win, and every criterion has a test; any other card waits for the human merge, as under merge. An agent cannot change this key through cerne settings: it is the project owner's decision. The guard stops the cerne settings command and asks in the Settings screen; an agent that edits .cernerc by hand or calls the local settings API of cerne ui still changes it.",
	},
	{
		ID: "gates.approve", Group: "process", Title: "Spec approval", Type: "string", Default: GateHuman, Enum: []string{GateHuman, GateAuto},
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "Who approves a spec. A person (human): only a person approves; an agent is refused. Automatic (auto): an agent may approve too, and a card an agent approves is marked approved without human review. An agent cannot change this key through cerne settings: it is the project owner's decision. The guard stops the cerne settings command and asks in the Settings screen; an agent that edits .cernerc by hand or calls the local settings API of cerne ui still changes it.",
	},
	{
		ID: "gates.gauntlet", Group: "process", Title: "Gauntlet", Type: "string", Default: "on", Enum: []string{"on", "off"},
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "Turns the Gauntlet on or off for this project. With off (light mode), approve accepts a spec without a bar, complete accepts a result without a ## Gauntlet section, and lint stops charging both. Only the literal off turns it off; any other stored value counts as on. An agent cannot change this key through cerne settings: it is the project owner's decision.",
	},
	{
		ID: "gates.risk_human_facets", Group: "process", Title: "Facets that always need a person", Type: "list", Default: defaultRiskHumanFacets,
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "Under the risk accept gate, a card that carries one of these facets always waits for a person, whatever its diff touches; a card with no facet waits too. Changed, the list counts from the next complete. An agent cannot change this key through cerne settings: it is the project owner's decision.",
	},
	{
		ID: "gates.risk_human_paths", Group: "process", Title: "Paths that always need a person", Type: "list", Default: defaultRiskHumanPaths, MaxLen: 256,
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "Under the risk accept gate, a delivery that changes a file matching one of these globs always waits for a person; the files are the git diff from the merge-base with the default branch to HEAD, and a diff that cannot be read waits too. A glob with no slash matches a file or folder name at any depth (go.mod, CLAUDE.md, migrations); one with a slash starts at the repository root (a leading / or ./ is dropped, a trailing / is the whole folder); * stays inside a folder, ** crosses folders, and case is ignored. The default covers the CI workflows of the three forges, dependency manifests and lockfiles, migrations, .cernerc and the agents' hooks and configs. Changed, the list counts from the next complete. An agent cannot change this key through cerne settings: it is the project owner's decision.",
	},
	{
		ID: "graph.enabled", Group: "interface", Title: "Graph", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Shows the Graph tab in the UI. Off hides it.",
	},
	{
		ID: "lint.cli_output_language", Group: "process", Title: "CLI output language", Type: "string", Default: "", Enum: []string{"en"},
		Scopes:      []string{ScopeProject},
		Description: "Set to en when the project's CLI prints English: lint then warns when a spec criterion quotes a CLI message that reads Portuguese. Empty turns the check off.",
	},
	{
		ID: "lint.criterion_tests", Group: "process", Title: "Criterion tests", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject},
		Description: "Charges every numbered spec criterion with a test that marks it (a comment such as F-0473 crit. 2) and every bug with a B-NNNN test: lint reports criterion-without-test and orphan-criterion-test, and complete refuses while they stand. Off stops both. Independent of light mode; test_roots and test_patterns in .cernerc say where the tests are.",
	},
	{
		ID: "lint.stale_decision_days", Group: "process", Title: "Stale decision window", Type: "int", Default: int64(180),
		Scopes: []string{ScopeProject}, TopLevel: "stale_decision_days",
		Description: "Days an active decision may go unreviewed, cited only by rejected or superseded features and by nothing live, before lint flags it as stale-decision. In .cernerc, [lint] stale_decision_days wins over a top-level stale_decision_days.",
	},
	{
		ID: "llm.base_url", Group: "ai", Title: "API base URL", Type: "string", Default: "", MaxLen: 256,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "The API endpoint for the OpenAI-compatible providers, for example a local Ollama (http://localhost:11434/v1). Required for the compatible provider; empty uses the provider's own address.",
	},
	{
		ID: "llm.mode", Group: "ai", Title: "AI agent", Type: "string", Default: "delegation",
		Enum:   []string{"claude-cli", "codex-cli", "gemini-cli", "cursor-cli", "copilot-cli", "byo-key", "delegation"},
		Scopes: []string{ScopeProject, ScopeHost},
		DynamicDefault: func() string {
			if cli := DetectSubscriptionCLI(); cli != "" {
				return CLIMode(cli)
			}
			return ""
		},
		Description: "How cerne reaches a model for ask, compile and the chat. A subscription CLI (claude-cli, codex-cli, gemini-cli, cursor-cli, copilot-cli) uses the plan you already pay for; byo-key calls a provider's API with a key; delegation hands the prompt to the agent already running. Unset, it follows the subscription CLI found on PATH.",
	},
	{
		ID: "llm.model", Group: "ai", Title: "Model", Type: "string", Default: "", MaxLen: 256,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "The model id sent to the CLI or the API. Empty uses the CLI's own default, or the catalog default for the anthropic provider.",
	},
	{
		ID: "llm.provider", Group: "ai", Title: "API provider", Type: "string", Default: "anthropic",
		Enum:        []string{"anthropic", "openai", "gemini", "openrouter", "compatible"},
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "The provider behind byo-key. gemini and openrouter use their own base URL and key variable; compatible takes any OpenAI-style endpoint through the API base URL, with an optional key.",
	},
	{
		ID: "mindmap.enabled", Group: "interface", Title: "Mindmap", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "Shows the Mindmap tab in the UI. Off hides it.",
	},
	{
		ID: "mirror.close_on", Group: "integrations", Title: "Close the issue on", Type: "string", Default: "accept", Enum: []string{"accept", "merge"},
		Scopes:      []string{ScopeProject},
		Description: "accept closes a mirrored card's issue when a human accepts the card. merge writes Closes: in the delivery commit, so the forge closes the issue when the PR merges, and mirror sync never reopens it.",
	},
	{
		ID: "mirror.enabled", Group: "integrations", Title: "Issue mirror", Type: "bool", Default: false,
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "On lets a human publish a card to an issue tracker with cerne feature mirror, and keeps each mirrored card's issue in step with the card's state. Off sends nothing to any tracker. An agent cannot change this key through cerne settings: it is the project owner's decision.",
	},
	{
		ID: "mirror.trackers", Group: "integrations", Title: "Issue trackers", Type: "records", Default: []map[string]string{},
		Scopes: []string{ScopeProject}, HumanOnly: true,
		Description: "The trackers a card can be mirrored to: a unique name, the kind, the https address of the issues repository (https://<host>/<owner>/<repo>) and an optional label that picks which outside issues mirror sync turns into ideas. The first one is the default of cerne feature mirror. An agent cannot change this key through cerne settings: it is the project owner's decision.",
		Fields: []Setting{
			{ID: "name", Title: "Name", Type: "string", Description: "A unique name for this tracker, used by --tracker."},
			{ID: "kind", Title: "Kind", Type: "string", Enum: tracker.KindNames(), Description: "The tracker's kind."},
			{ID: "url", Title: "URL", Type: "string", MaxLen: 256, Description: "The https address of the issues repository."},
			{ID: "label", Title: "Label", Type: "string", Description: "Only outside issues with this label become ideas. Empty imports every open issue."},
		},
		CheckRecord: func(r map[string]string) error {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r["label"])), "cerne:") {
				return fmt.Errorf("label %q: the cerne: prefix is reserved for the mirror's state labels", r["label"])
			}
			_, err := tracker.New(trackerConfig(r))
			return err
		},
	},
	{
		ID: "references.archive", Group: "integrations", Title: "Archive public sources", Type: "bool", Default: false,
		Scopes: []string{ScopeProject, ScopeHost}, HumanOnly: true,
		Description: "On, cerne reference new and cerne reference archive send the URL of an external reference, and nothing else, to the Wayback Machine of the Internet Archive and keep the link of the dated copy. Only a page that answers 200 to an anonymous request is sent: never an internal host, an ignored source, a share-link host, a URL with a secret or credentials, or a page an anonymous request cannot read; a reference registered with --no-archive is never sent. A private page that answers 200 to anyone holding its link still passes, so review cerne reference archive --dry-run before the first real run. Off sends nothing, and a project that stores off (or a value that is not true) stays off whatever the machine scope says. An agent cannot change this key through cerne settings: it is the project owner's decision.",
	},
	{
		ID: "ui.update_check", Group: "interface", Title: "Check for a new release", Type: "bool", Default: true,
		Scopes:      []string{ScopeHost},
		Description: "On, the cerne ui panel asks GitHub for the latest release when it starts and every 24 hours while it is open, and shows an Update button when a newer one exists. Off, the panel never asks; cerne update still checks when you run it. It applies to every project on this machine.",
	},
	{
		ID: "wiki.enabled", Group: "interface", Title: "Wiki", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "On keeps the wiki live: when compile_on_push is set, a push recompiles the dirty pages, and wiki lint debt counts as pending work in 'cerne next'. Off stops both; the wiki stays readable, with a stale banner and an on-demand recompile.",
	},
	{
		ID: "links.suggest", Group: "ai", Title: "Suggest links with the LLM", Type: "bool", Default: false,
		Scopes: []string{ScopeProject, ScopeHost}, HumanOnly: true,
		Description: "On lets cerne links suggest ask the configured LLM for relations a document mentions without a link, after showing the documents and a token estimate; a person accepts or rejects each one. Off refuses the command. Nothing else in cerne asks for suggestions. An agent cannot change this key through cerne settings: spending tokens is the project owner's decision.",
	},
	{
		ID: "hooks.notes_on_edit", Group: "integrations", Title: "Notes on edit", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "On shows the agent the notes that cite a file or declare it in an edit: trigger when it edits that file: before the edit on Claude Code and Codex, right after it on Cursor, Gemini and Copilot. Off stops showing them.",
	},
	{
		ID: "hooks.notes_on_bash", Group: "integrations", Title: "Notes on shell commands", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "On shows the agent up to 3 active notes whose bash: trigger matches a shell command it runs: before the command on Claude Code and Codex, right after it on Cursor, Gemini and Copilot. Off stops showing them.",
	},
	{
		ID: "hooks.prompt_notes", Group: "integrations", Title: "Notes on prompt", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "On shows the agent up to 3 active notes or runbooks whose titles match the words of the user's prompt, once per session, through the prompt hook. Off stops showing them.",
	},
	{
		ID: "hooks.auto_refresh", Group: "integrations", Title: "Refresh hooks and guide", Type: "bool", Default: true,
		Scopes:      []string{ScopeProject, ScopeHost},
		Description: "On renews an agent's hooks and authoring guide by themselves, at the session start and in cerne next, when every managed file is one a past cerne release wrote and none was edited; the change applies from the next session. Off leaves them alone and only warns that they are out of date (cerne update renews them).",
	},
}

// WikiEnabled is wiki.enabled for the project at (or above) projDir (F-0316):
// false only when it resolves to false. A value the resolution skipped — an
// invalid one, or a scope file it could not read — falls through as F-0314
// resolves it, but never silently: warning is then one line naming each
// problem and the value used instead ("" when there is none).
func WikiEnabled(projDir string) (on bool, warning string) {
	view := ResolveSettings(projDir)
	for _, s := range view.Settings {
		if s.ID != "wiki.enabled" {
			continue
		}
		var problems []string
		for _, p := range s.Invalid {
			problems = append(problems, fmt.Sprintf("invalid in %s scope (%s)", p.Scope, p.File))
		}
		for _, p := range view.Errors {
			problems = append(problems, fmt.Sprintf("unreadable in %s scope (%s)", p.Scope, p.File))
		}
		if len(problems) > 0 {
			warning = fmt.Sprintf("warning: wiki.enabled %s; using %s %v", strings.Join(problems, ", "), s.Origin, s.Value)
		}
		return s.Value != false, warning
	}
	return true, ""
}

// LinksSuggest is links.suggest for the project at (or above) projDir
// (F-0502): true only when it resolves to true.
func LinksSuggest(projDir string) (bool, error) {
	view := ResolveSettings(projDir)
	if len(view.Errors) > 0 {
		return false, fmt.Errorf("links.suggest: unreadable in %s scope (%s)", view.Errors[0].Scope, view.Errors[0].File)
	}
	for _, s := range view.Settings {
		if s.ID == "links.suggest" {
			return s.Value == true, nil
		}
	}
	return false, nil
}

// PromptNotes is hooks.prompt_notes for the project at (or above) projDir
// (B-0020): false only when it resolves to false.
func PromptNotes(projDir string) bool {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "hooks.prompt_notes" {
			return s.Value != false
		}
	}
	return true
}

// ReferencesArchive is references.archive for the project at (or above)
// projDir (F-0503): true only when it resolves to true and the project scope
// says nothing else. A project value that is not true (false, or one the
// resolution skips as invalid) or an unreadable project file keeps it off,
// whatever the host scope stores.
func ReferencesArchive(projDir string) bool {
	view := ResolveSettings(projDir)
	for _, p := range view.Errors {
		if p.Scope == ScopeProject {
			return false
		}
	}
	for _, s := range view.Settings {
		if s.ID != "references.archive" {
			continue
		}
		if v, ok := s.Stored[ScopeProject]; ok && v != true {
			return false
		}
		for _, p := range s.Invalid {
			if p.Scope == ScopeProject {
				return false
			}
		}
		return s.Value == true
	}
	return false
}

// UpdateCheck is ui.update_check (F-0480), a host setting read through the
// project at (or above) projDir: false only when it resolves to false.
func UpdateCheck(projDir string) bool {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "ui.update_check" {
			return s.Value != false
		}
	}
	return true
}

// NotesOnEdit is hooks.notes_on_edit for the project at (or above) projDir
// (F-0366): false only when it resolves to false.
func NotesOnEdit(projDir string) bool {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "hooks.notes_on_edit" {
			return s.Value != false
		}
	}
	return true
}

// AutoRefresh is hooks.auto_refresh for the project at (or above) projDir
// (F-0477): false only when it resolves to false.
func AutoRefresh(projDir string) bool {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "hooks.auto_refresh" {
			return s.Value != false
		}
	}
	return true
}

// NotesOnBash is hooks.notes_on_bash for the project at (or above) projDir
// (F-0457): false only when it resolves to false.
func NotesOnBash(projDir string) bool {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "hooks.notes_on_bash" {
			return s.Value != false
		}
	}
	return true
}

// ResolveSettings resolves every declared setting for the project at (or
// above) projDir: project > host > default. It never fails as a whole: an
// unreadable scope file is reported once in Errors and leaves that scope
// empty; a stored value outside the key's type or in a scope the key does not
// declare (F-0333: a .cernerc naming a host-only key) is reported on that key,
// which falls through to the next valid scope. No .cernerc leaves the project
// scope empty; an absent host file leaves the host scope empty.
func ResolveSettings(projDir string) SettingsView {
	scopes := []string{ScopeProject, ScopeHost}
	view := SettingsView{Files: map[string]string{}}
	layers := map[string]map[string]any{}
	for _, scope := range scopes {
		path, err := settingsFile(projDir, scope)
		if scope == ScopeProject && errors.Is(err, ErrNoProject) {
			continue
		}
		if err == nil {
			view.Files[scope] = path
			layers[scope], err = readTOMLFile(path)
		}
		if err != nil {
			view.Errors = append(view.Errors, SettingProblem{Scope: scope, File: path, Error: err.Error()})
		}
	}
	for _, s := range settingsRegistry {
		r := ResolvedSetting{Setting: s, Value: s.Default, Origin: OriginDefault}
		if s.DynamicDefault != nil {
			if v := s.DynamicDefault(); v != "" {
				r.Value, r.Origin = v, OriginDetected
			}
		}
		table, key, _ := strings.Cut(s.ID, ".")
		for _, scope := range scopes {
			tbl, _ := layers[scope][table].(map[string]any)
			v, ok := tbl[key]
			if !ok && scope == ScopeProject && s.TopLevel != "" {
				// The [table] key wins; the top-level one is read only without it.
				v, ok = layers[scope][s.TopLevel]
			}
			if !ok {
				continue
			}
			problem := ""
			if err := checkDeclared(s.ID, scope); err != nil {
				problem = err.Error()
			} else if s.Type == "records" {
				if rows, err := checkRecords(s, v); err != nil {
					problem = err.Error()
				} else {
					v = rows
				}
			} else if s.Type == "list" {
				if items, err := checkList(s, v); err != nil {
					problem = err.Error()
				} else {
					v = items
				}
			} else if !hasSettingType(s, v) {
				problem = fmt.Sprintf("%v: want %s", ErrInvalidSettingValue, settingWant(s))
			}
			if problem != "" {
				raw := fmt.Sprint(v)
				if str, isStr := v.(string); isStr {
					raw = strconv.Quote(str)
				}
				r.Invalid = append(r.Invalid, SettingProblem{Scope: scope, File: view.Files[scope], Raw: raw, Error: problem})
				continue
			}
			if r.Stored == nil {
				r.Stored = map[string]any{}
			}
			r.Stored[scope] = v
			if r.Origin == OriginDefault || r.Origin == OriginDetected {
				r.Value, r.Origin = v, scope
			}
		}
		view.Settings = append(view.Settings, r)
	}
	return view
}

// hasSettingType reports whether v is a value of s's declared type.
func hasSettingType(s Setting, v any) bool {
	if s.Type == "records" {
		_, err := checkRecords(s, v)
		return err == nil
	}
	if s.Type == "list" {
		_, err := checkList(s, v)
		return err == nil
	}
	switch v.(type) {
	case bool:
		return s.Type == "bool"
	case string:
		return s.Type == "string" && (len(s.Enum) == 0 || slices.Contains(s.Enum, v.(string)))
	case int64:
		return s.Type == "int" && v.(int64) > 0
	}
	return false
}

// settingWant names the type a refusal wants: "string", or the enum's values.
func settingWant(s Setting) string {
	if len(s.Enum) > 0 {
		return "one of: " + strings.Join(s.Enum, ", ")
	}
	if s.Type == "int" {
		return "a positive integer"
	}
	if s.Type == "records" {
		return "a list of records {" + strings.Join(fieldIDs(s), ", ") + "}"
	}
	if s.Type == "list" {
		return "a list of strings"
	}
	return s.Type
}

// CLIOutputLanguage is lint.cli_output_language for the project at (or above)
// projDir (F-0353): "en" when the project declares its CLI output English, ""
// otherwise — unset, invalid or outside a project.
func CLIOutputLanguage(projDir string) string {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "lint.cli_output_language" {
			v, _ := s.Value.(string)
			return v
		}
	}
	return ""
}

// CriterionTestsEnabled is lint.criterion_tests for the project at (or above)
// projDir (F-0473): on unless a scope turns it off.
func CriterionTestsEnabled(projDir string) bool {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "lint.criterion_tests" {
			if v, ok := s.Value.(bool); ok {
				return v
			}
		}
	}
	return true
}

// StaleDecisionDays is lint.stale_decision_days for the project at (or above)
// projDir (F-0385): [lint] stale_decision_days, else the top-level
// stale_decision_days of .cernerc, else 180 — one resolution for lint, next,
// push and the ui.
func StaleDecisionDays(projDir string) int {
	for _, s := range ResolveSettings(projDir).Settings {
		if s.ID == "lint.stale_decision_days" {
			if n, ok := s.Value.(int64); ok {
				return int(n)
			}
		}
	}
	return 180
}

// maxSettingString bounds a string setting (a display name, F-0325).
const maxSettingString = 64

func maxLen(s Setting) int {
	if s.MaxLen > 0 {
		return s.MaxLen
	}
	return maxSettingString
}

// SetSetting writes the CLI literal raw as id's value in scope: "true"/"false"
// for a bool key, a JSON list for a records key, the text itself for a string
// key. See SetSettingValue.
func SetSetting(projDir, id, scope, raw string) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	var v any = raw
	switch declared(id).Type {
	case "bool":
		if raw != "true" && raw != "false" {
			return fmt.Errorf("%s: %w %q: want true or false", id, ErrInvalidSettingValue, raw)
		}
		v = raw == "true"
	case "int":
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %w %q: want a positive integer", id, ErrInvalidSettingValue, raw)
		}
		v = n
	case "records":
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return fmt.Errorf("%s: %w %q: want a JSON list of records", id, ErrInvalidSettingValue, raw)
		}
	case "list":
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return fmt.Errorf("%s: %w %q: want a JSON list of strings", id, ErrInvalidSettingValue, raw)
		}
	}
	return SetSettingValue(projDir, id, scope, v)
}

// SetSettingValue writes v as id's value in scope: the project's .cernerc or
// the host settings.toml, editing only that key's line. Key, scope and value
// (its type; a string is one non-blank line of at most 64 runes) are
// validated before any file is read, so a refusal writes nothing.
func SetSettingValue(projDir, id, scope string, v any) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	s := declared(id)
	// A JSON number decodes as float64: a whole one is an int key's value.
	if f, ok := v.(float64); ok && s.Type == "int" && f == float64(int64(f)) {
		v = int64(f)
	}
	if s.Type == "records" {
		rows, err := checkRecords(s, v)
		if err != nil {
			return err
		}
		v = inlineRecords{fields: fieldIDs(s), rows: rows}
	} else if s.Type == "list" {
		items, err := checkList(s, v)
		if err != nil {
			return err
		}
		v = items
	} else if !hasSettingType(s, v) {
		return fmt.Errorf("%s: %w %v: want %s", id, ErrInvalidSettingValue, v, settingWant(s))
	}
	if str, ok := v.(string); ok && (strings.TrimSpace(str) == "" || strings.ContainsAny(str, "\r\n") || utf8.RuneCountInString(str) > maxLen(s)) {
		return fmt.Errorf("%s: %w %q: want one non-blank line of at most %d characters", id, ErrInvalidSettingValue, str, maxLen(s))
	}
	path, err := settingsFile(projDir, scope)
	if err != nil {
		return err
	}
	table, key, _ := strings.Cut(id, ".")
	_, err = editTOMLFile(path, table, key, v)
	return err
}

// UnsetSetting removes id from scope's file, handing the key back to the next
// scope down (host, then the default). A key the file does not hold is
// ErrSettingNotSet, and nothing is written.
func UnsetSetting(projDir, id, scope string) error {
	if err := checkDeclared(id, scope); err != nil {
		return err
	}
	path, err := settingsFile(projDir, scope)
	if err != nil {
		return err
	}
	table, key, _ := strings.Cut(id, ".")
	removed, err := editTOMLFile(path, table, key, nil)
	if err == nil && !removed {
		return fmt.Errorf("%w: %s is not set in %s scope", ErrSettingNotSet, id, scope)
	}
	return err
}

// declared returns id's declaration; id must have passed checkDeclared.
func declared(id string) Setting {
	return settingsRegistry[slices.IndexFunc(settingsRegistry, func(s Setting) bool { return s.ID == id })]
}

// checkDeclared refuses an id the registry does not declare, or a scope the
// setting does not allow.
func checkDeclared(id, scope string) error {
	i := slices.IndexFunc(settingsRegistry, func(s Setting) bool { return s.ID == id })
	if i < 0 {
		ids := make([]string, len(settingsRegistry))
		for j, s := range settingsRegistry {
			ids[j] = s.ID
		}
		return fmt.Errorf("%w %q (known: %s)", ErrUnknownSetting, id, strings.Join(ids, ", "))
	}
	if scopes := settingsRegistry[i].Scopes; !slices.Contains(scopes, scope) {
		return fmt.Errorf("%w %q (allowed: %s)", ErrSettingScope, scope, strings.Join(scopes, ", "))
	}
	return nil
}

// editTOMLFile applies editTOML to the file at path (absent = empty) under
// git's lockfile discipline, and reports whether it wrote: <path>.lock, created O_EXCL, is the lock and
// receives the new content, then is renamed over the file. Writers in this
// process (the ui) and in others (the CLI) serialize instead of losing
// updates; one still blocked after configLockTimeout gets ErrConfigBusy and
// writes nothing. A foreign lock is never removed: a crashed writer's stale
// lock is the user's to delete, as with git. No change skips the write. Like
// WriteFileAtomic, it writes through a symlink and keeps the file's mode.
func editTOMLFile(path, table, key string, value any) (bool, error) {
	if _, err := os.Stat(path); value == nil && errors.Is(err, fs.ErrNotExist) {
		return false, nil // removing from a missing file: nothing to lock or create
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	// Write to the symlink's target; messages keep the path the user gave.
	target, mode, err := writeTarget(path)
	if err != nil {
		return false, err
	}
	lock := target + ".lock"
	deadline := time.Now().Add(configLockTimeout)
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	for errors.Is(err, fs.ErrExist) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		f, err = os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	}
	if errors.Is(err, fs.ErrExist) {
		return false, fmt.Errorf("%s: %w; if no cerne is running, remove %s", path, ErrConfigBusy, lock)
	}
	if err != nil {
		return false, err
	}
	renamed := false
	defer func() {
		f.Close()
		if !renamed {
			os.Remove(lock)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return false, err
	}

	raw, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	out, err := editTOML(raw, table, key, value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	if bytes.Equal(out, raw) {
		return false, nil
	}
	if _, err := f.Write(out); err != nil {
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(lock, target); err != nil {
		return false, err
	}
	renamed = true
	return true, nil
}

// settingsFile returns the file behind scope: the .cernerc found from
// projDir (not decoded: settings read it generically), or
// <ConfigDir>/settings.toml.
func settingsFile(projDir, scope string) (string, error) {
	if scope == ScopeHost {
		dir, err := ConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, hostSettingsFilename), nil
	}
	dir, err := findConfigDir(projDir)
	if err != nil {
		return "", fmt.Errorf("project scope: %w", err)
	}
	return filepath.Join(dir, configFilename), nil
}

// readTOMLFile decodes path as a generic TOML document; absent reads as empty.
func readTOMLFile(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrConfigParse, err)
	}
	return m, nil
}

// checkRecords validates v, a JSON list or a TOML array of tables, as a value
// of the records key s, and normalizes it: one map per record, string values,
// the empty ones dropped (the writer drops them too). A field s does not
// declare, a value that is not one line of at most the field's MaxLen, an
// empty or repeated Fields[0], or a CheckRecord refusal is
// ErrInvalidSettingValue.
func checkRecords(s Setting, v any) ([]map[string]string, error) {
	var items []any
	switch l := v.(type) {
	case []any:
		items = l
	case []map[string]any:
		for _, m := range l {
			items = append(items, m)
		}
	case []map[string]string:
		for _, m := range l {
			items = append(items, m)
		}
	default:
		return nil, fmt.Errorf("%s: %w: want %s", s.ID, ErrInvalidSettingValue, settingWant(s))
	}
	bad := func(i int, format string, a ...any) error {
		return fmt.Errorf("%s: %w: record %d: %s", s.ID, ErrInvalidSettingValue, i+1, fmt.Sprintf(format, a...))
	}
	key := s.Fields[0].ID
	out := make([]map[string]string, 0, len(items))
	seen := map[string]bool{}
	for i, item := range items {
		rec := map[string]string{}
		var fields map[string]any
		switch m := item.(type) {
		case map[string]any:
			fields = m
		case map[string]string:
			fields = map[string]any{}
			for k, val := range m {
				fields[k] = val
			}
		default:
			return nil, bad(i, "want an object of %s", settingWant(s))
		}
		for name, val := range fields {
			j := slices.IndexFunc(s.Fields, func(f Setting) bool { return f.ID == name })
			if j < 0 {
				return nil, bad(i, "unknown field %q (fields: %s)", name, strings.Join(fieldIDs(s), ", "))
			}
			str, ok := val.(string)
			if !ok {
				return nil, bad(i, "%s: want a string", name)
			}
			// Below 0x20 and DEL: strconv.Quote would write an escape (\x01, \a)
			// that the TOML parser then rejects.
			if strings.ContainsFunc(str, func(r rune) bool { return r < 0x20 || r == 0x7f }) || utf8.RuneCountInString(str) > maxLen(s.Fields[j]) {
				return nil, bad(i, "%s: want one line of at most %d characters, with no control character", name, maxLen(s.Fields[j]))
			}
			if str != "" {
				rec[name] = str
			}
		}
		switch {
		case strings.TrimSpace(rec[key]) == "":
			return nil, bad(i, "%s is required", key)
		case seen[rec[key]]:
			return nil, bad(i, "duplicate %s %q", key, rec[key])
		}
		seen[rec[key]] = true
		if s.CheckRecord != nil {
			if err := s.CheckRecord(rec); err != nil {
				return nil, fmt.Errorf("%s: %w: %w", s.ID, ErrInvalidSettingValue, err)
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// checkList validates v, a JSON list or a TOML array, as a value of the list
// key s and normalizes it to its strings (F-0516). An item that is not one
// non-blank line of at most the key's MaxLen, with no control character, is
// ErrInvalidSettingValue. An empty list is a value: it clears the default.
func checkList(s Setting, v any) ([]string, error) {
	var items []any
	switch l := v.(type) {
	case []any:
		items = l
	case []string:
		for _, x := range l {
			items = append(items, x)
		}
	default:
		return nil, fmt.Errorf("%s: %w: want %s", s.ID, ErrInvalidSettingValue, settingWant(s))
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		str, ok := item.(string)
		if !ok || strings.TrimSpace(str) == "" || strings.ContainsFunc(str, func(r rune) bool { return r < 0x20 || r == 0x7f }) || utf8.RuneCountInString(str) > maxLen(s) {
			return nil, fmt.Errorf("%s: %w: item %d: want one non-blank line of at most %d characters, with no control character", s.ID, ErrInvalidSettingValue, i+1, maxLen(s))
		}
		if strings.Contains(str, `\`) {
			return nil, fmt.Errorf("%s: %w: item %d %q: %w", s.ID, ErrInvalidSettingValue, i+1, str, ErrSettingBackslash)
		}
		out = append(out, str)
	}
	return out, nil
}

// fieldIDs lists the columns of a records key, in order.
func fieldIDs(s Setting) []string {
	ids := make([]string, len(s.Fields))
	for i, f := range s.Fields {
		ids[i] = f.ID
	}
	return ids
}
