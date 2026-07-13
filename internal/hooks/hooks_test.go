package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_CreatesWhenAbsent (C7): with no .claude/, install writes the
// script and a fresh settings.json wiring the hook; both report created.
func TestInstall_CreatesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", hookScript)); err != nil {
		t.Fatalf("script not installed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), hookScript) {
		t.Fatalf("settings.json missing hook wiring:\n%s", b)
	}
	if !isValidJSON(b) {
		t.Fatalf("settings.json is not valid JSON:\n%s", b)
	}
	for _, r := range results {
		if r.Status != StatusCreated {
			t.Errorf("%s: status=%s, want created", r.Asset, r.Status)
		}
	}
}

// TestInstall_MergesPreservingExisting (C7): a user's existing hooks and other
// top-level keys survive the merge; our PostToolUse entry is added.
func TestInstall_MergesPreservingExisting(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "SessionStart": [{"hooks":[{"type":"command","command":"echo hi"}]}],
    "PreToolUse": [{"matcher":"Write","hooks":[{"type":"command","command":"echo w"}]}]
  }
}`
	if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(filepath.Join(claude, "settings.json"))
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("merged settings.json invalid: %v\n%s", err, b)
	}
	if _, ok := got["permissions"]; !ok {
		t.Errorf("top-level 'permissions' key lost:\n%s", b)
	}
	hooks, _ := got["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; !ok {
		t.Errorf("existing SessionStart hook lost:\n%s", b)
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Errorf("existing PreToolUse hook lost:\n%s", b)
	}
	if !strings.Contains(string(b), hookScript) {
		t.Errorf("our hook not added:\n%s", b)
	}
	// preserved the user's command payloads
	if !strings.Contains(string(b), "echo hi") || !strings.Contains(string(b), "echo w") {
		t.Errorf("user hook commands lost:\n%s", b)
	}
}

// TestInstall_Idempotent (C7): a second install changes nothing and never
// duplicates the wiring.
func TestInstall_Idempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged {
			t.Errorf("second install %s: status=%s, want unchanged", r.Asset, r.Status)
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if n := strings.Count(string(b), hookScript); n != 1 {
		t.Errorf("hook wired %d times, want 1:\n%s", n, b)
	}
}

// TestInstall_FailsOnCorruptJSON (C7): a corrupt settings.json is fail-closed —
// install errors and leaves the file untouched.
func TestInstall_FailsOnCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("{ this is not json ")
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err == nil {
		t.Fatalf("expected error on corrupt settings.json")
	}
	b, _ := os.ReadFile(sp)
	if string(b) != string(corrupt) {
		t.Errorf("corrupt settings.json was modified (not fail-closed):\n%s", b)
	}
}

// TestInstall_MinimalDiff_AppendsToExistingPostToolUse (F-0106, env merge
// F-0131): merging into a rich, tracked settings.json must append ONLY the
// new Bash entry and the CERNE_ACTOR env key — every other byte (key order,
// inline objects, indentation, &/non-ASCII escaping) stays identical.
// Asserted by exact-string equality against the surgically-inserted
// expectation.
func TestInstall_MinimalDiff_AppendsToExistingPostToolUse(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	// Keys deliberately out of alphabetical order (permissions, env, hooks);
	// an inline object; a value with '&'; a non-ASCII value ("Olá").
	pre := `{
  "permissions": {"allow": ["Bash(ls:*)"], "deny": ["Read(./secret&stuff)"]},
  "env": {"GREETING": "Olá & tchau"},
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write",
        "hooks": [
          {
            "type": "command",
            "command": "echo existing"
          }
        ]
      }
    ]
  }
}
`
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(sp)

	// The new Bash entries (provenance + work-bind), indented as siblings
	// (6 spaces) of the Write entry.
	bashBlock := `      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/register-reference-reminder.sh\""
          }
        ]
      }`
	bindBlock := `      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-bind.sh\""
          }
        ]
      }`
	// The lifecycle reminder block (F-0170), appended after work-bind.
	lifecycleBlock := `      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-lifecycle-reminder.sh\""
          }
        ]
      }`
	// The SessionStart reminder block (F-0139), inserted as a new member of the
	// hooks object after PostToolUse.
	ssBlock := `    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-register-reminder.sh\""
          }
        ]
      }
    ]`
	// The SubagentStart context block (F-0150), appended after SessionStart.
	sbBlock := `    "SubagentStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-subagent-context.sh\""
          }
        ]
      }
    ]`
	// The PreToolUse work-gate block (F-0151), appended after SubagentStart.
	preBlock := `    "PreToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-gate.sh\""
          }
        ]
      }
    ]`
	// The Stop closeout block (F-0171), appended after PreToolUse.
	stopBlock := `    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-stop-closeout.sh\""
          }
        ]
      }
    ]`
	// Minimal diff: the Write entry's closing brace gains a comma; the Bash
	// blocks (provenance + work-bind) are inserted before PostToolUse's closing
	// bracket; the SessionStart, SubagentStart and PreToolUse members are
	// appended to the hooks object; the CERNE_ACTOR key is appended inside the
	// existing inline env object. Nothing else moves.
	want := strings.Replace(pre, "\n      }\n    ]", "\n      },\n"+bashBlock+",\n"+bindBlock+",\n"+lifecycleBlock+"\n    ]", 1)
	want = strings.Replace(want, "\n    ]\n  }\n}", "\n    ],\n"+ssBlock+",\n"+sbBlock+",\n"+preBlock+",\n"+stopBlock+"\n  }\n}", 1)
	want = strings.Replace(want, `"env": {"GREETING": "Olá & tchau"},`,
		"\"env\": {\"GREETING\": \"Olá & tchau\",\n    \"CERNE_ACTOR\": \"agent\"},", 1)

	if string(got) != want {
		t.Fatalf("merge was not a minimal diff.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if !isValidJSON(got) {
		t.Fatalf("merged settings.json is not valid JSON:\n%s", got)
	}
	// Escaping preserved: literal '&' and non-ASCII survive untouched (a map
	// round-trip through json.MarshalIndent would HTML-escape '&' to &).
	if strings.Contains(string(got), "\\u0026") {
		t.Errorf("'&' was HTML-escaped to \\u0026:\n%s", got)
	}
	if !strings.Contains(string(got), "secret&stuff") || !strings.Contains(string(got), "Olá & tchau") {
		t.Errorf("literal '&' / non-ASCII not preserved:\n%s", got)
	}
	// Key order preserved (would be reordered alphabetically by a map round-trip).
	permIdx := strings.Index(string(got), `"permissions"`)
	envIdx := strings.Index(string(got), `"env"`)
	hooksIdx := strings.Index(string(got), `"hooks"`)
	if !(permIdx < envIdx && envIdx < hooksIdx) {
		t.Errorf("top-level key order not preserved (perm=%d env=%d hooks=%d):\n%s", permIdx, envIdx, hooksIdx, got)
	}
}

// TestInstall_MinimalDiff_ScenarioB (F-0106): hooks exists without PostToolUse —
// the PostToolUse key is inserted into the hooks object, every other byte
// (including an inline PreToolUse entry) preserved.
func TestInstall_MinimalDiff_ScenarioB(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "PreToolUse": [{"matcher":"Write","hooks":[{"type":"command","command":"echo w"}]}]
  }
}
`
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(sp)

	want := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "PreToolUse": [{"matcher":"Write","hooks":[{"type":"command","command":"echo w"}]},
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-gate.sh\""
          }
        ]
      }],
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/register-reference-reminder.sh\""
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-bind.sh\""
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-lifecycle-reminder.sh\""
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-register-reminder.sh\""
          }
        ]
      }
    ],
    "SubagentStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-subagent-context.sh\""
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-stop-closeout.sh\""
          }
        ]
      }
    ]
  },
  "env": {
    "CERNE_ACTOR": "agent"
  }
}
`
	if string(got) != want {
		t.Fatalf("scenario B not a minimal diff.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// The pre-existing inline PreToolUse entry survives verbatim (its line only
	// gains the comma before the appended work-gate entry).
	if !strings.Contains(string(got), `    "PreToolUse": [{"matcher":"Write","hooks":[{"type":"command","command":"echo w"}]},`) {
		t.Errorf("inline PreToolUse entry not preserved verbatim:\n%s", got)
	}
}

// TestInstall_MinimalDiff_ScenarioC (F-0106): no hooks key — the hooks object is
// inserted into the root, other keys preserved.
func TestInstall_MinimalDiff_ScenarioC(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "permissions": {"allow": ["Bash(ls:*)"]}
}
`
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(sp)

	want := `{
  "permissions": {"allow": ["Bash(ls:*)"]},
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/register-reference-reminder.sh\""
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-bind.sh\""
          }
        ]
      },
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-lifecycle-reminder.sh\""
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-register-reminder.sh\""
          }
        ]
      }
    ],
    "SubagentStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-subagent-context.sh\""
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-work-gate.sh\""
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/cerne-stop-closeout.sh\""
          }
        ]
      }
    ]
  },
  "env": {
    "CERNE_ACTOR": "agent"
  }
}
`
	if string(got) != want {
		t.Fatalf("scenario C not a minimal diff.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	if !strings.Contains(string(got), `  "permissions": {"allow": ["Bash(ls:*)"]},`) {
		t.Errorf("permissions line not preserved verbatim:\n%s", got)
	}
}

// TestInstall_FailsOnNonArrayPostToolUse (F-0106): a PostToolUse that is not an
// array is malformed — fail closed rather than clobber it.
func TestInstall_FailsOnNonArrayPostToolUse(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := []byte(`{"hooks":{"PostToolUse":{}}}`)
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, pre, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err == nil {
		t.Fatalf("expected error on non-array PostToolUse")
	}
	if b, _ := os.ReadFile(sp); string(b) != string(pre) {
		t.Errorf("settings.json was modified (not fail-closed):\n%s", b)
	}
}

// TestInstall_IdempotentAfterRichMerge (F-0106): re-installing over an
// already-merged rich settings.json changes nothing (byte-identical, unchanged).
func TestInstall_IdempotentAfterRichMerge(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "env": {"GREETING": "Olá & tchau"},
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write",
        "hooks": [{"type":"command","command":"echo existing"}]
      }
    ]
  }
}
`
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(sp)
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Asset == "settings.json" && r.Status != StatusUnchanged {
			t.Errorf("second install settings.json: status=%s, want unchanged", r.Status)
		}
	}
	second, _ := os.ReadFile(sp)
	if string(first) != string(second) {
		t.Errorf("second install rewrote the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func isValidJSON(b []byte) bool {
	var v any
	return json.Unmarshal(b, &v) == nil
}

// TestInstall_WritesActorEnv (F-0131 crit. 11): a fresh install (no
// .claude/settings.json) writes env.CERNE_ACTOR=agent alongside the hook
// wiring — the marker `internal/app.EventActor`/`CheckHumanGate` read to tell
// an agent session apart from a human terminal.
func TestInstall_WritesActorEnv(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("settings.json invalid: %v\n%s", err, b)
	}
	env, _ := got["env"].(map[string]any)
	if env == nil || env["CERNE_ACTOR"] != "agent" {
		t.Fatalf("settings.json missing env.CERNE_ACTOR=agent:\n%s", b)
	}
}

// TestInstall_ActorEnvPreservesExistingEnvKeys (F-0131 crit. 11): installing
// over a settings.json that already has its own env keys adds CERNE_ACTOR
// without disturbing them.
func TestInstall_ActorEnvPreservesExistingEnvKeys(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, ".claude")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	pre := `{
  "env": {"MY_VAR": "keep-me", "OTHER": "also-keep"}
}
`
	sp := filepath.Join(claude, "settings.json")
	if err := os.WriteFile(sp, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(sp)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("merged settings.json invalid: %v\n%s", err, b)
	}
	env, _ := got["env"].(map[string]any)
	if env == nil {
		t.Fatalf("env key lost:\n%s", b)
	}
	if env["MY_VAR"] != "keep-me" || env["OTHER"] != "also-keep" {
		t.Errorf("existing env keys not preserved: %+v", env)
	}
	if env["CERNE_ACTOR"] != "agent" {
		t.Errorf("CERNE_ACTOR not added: %+v", env)
	}
}

// TestInstall_ActorEnvIdempotent: a second install over an already-marked
// settings.json changes nothing (byte-identical, unchanged status).
func TestInstall_ActorEnvIdempotent(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	sp := filepath.Join(dir, ".claude", "settings.json")
	first, _ := os.ReadFile(sp)

	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Asset == "settings.json" && r.Status != StatusUnchanged {
			t.Errorf("second install settings.json: status=%s, want unchanged", r.Status)
		}
	}
	second, _ := os.ReadFile(sp)
	if string(first) != string(second) {
		t.Errorf("second install rewrote the file:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

// TestInstall_WiresLifecycleReminder (F-0170 crit. 5): the lifecycle reminder
// script is installed and wired as a PostToolUse/Bash entry, idempotently.
func TestInstall_WiresLifecycleReminder(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", lifecycleScript)); err != nil {
		t.Fatalf("lifecycle script not installed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), lifecycleScript) {
		t.Fatalf("settings.json missing lifecycle wiring:\n%s", b)
	}
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged {
			t.Errorf("%s: status=%s, want up to date", r.Asset, r.Status)
		}
	}
}

// TestInstall_WiresStopCloseout (F-0171 crit. 6): the Stop closeout script is
// installed and wired as a Stop event entry, idempotently.
func TestInstall_WiresStopCloseout(t *testing.T) {
	dir := t.TempDir()
	if _, err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "hooks", stopScript)); err != nil {
		t.Fatalf("stop script not installed: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), stopScript) || !strings.Contains(string(b), `"Stop"`) {
		t.Fatalf("settings.json missing Stop wiring:\n%s", b)
	}
	results, err := Install(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Status != StatusUnchanged {
			t.Errorf("%s: status=%s, want up to date", r.Asset, r.Status)
		}
	}
}
