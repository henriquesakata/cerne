package verbs_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/cli"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/verbs"
)

// cerne runs the CLI in-process (fixture setup only).
func cerne(t *testing.T, args ...string) string {
	t.Helper()
	root := cli.Root()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("cerne %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out.String()
}

// newProject builds an initialized cerne project over a local bare origin and
// returns the Env the executor runs in, with the test binary as cerne.
func newProject(t *testing.T) verbs.Env {
	t.Helper()
	t.Setenv("CERNE_CACHE_DIR", t.TempDir())
	t.Setenv("CERNE_TEST_RUN_CLI", "1")
	origin := filepath.Join(t.TempDir(), "origin.git")
	if err := gitback.InitBare(origin); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	cerne(t, "init", "--docs-language", "pt-BR", "--name", "Verbs", "--backend", origin, "--dir", proj)
	cfg, _, err := project.LoadConfig(proj)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := project.CacheDir(cfg.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return verbs.Env{Bin: bin, ProjDir: proj, RepoDir: filepath.Join(cache, "repo"), Confirmed: true}
}

// run executes a call whose own command is the confirmed card — the real
// flow: the human confirmed exactly this command.
func run(t *testing.T, env verbs.Env, verb string, stdin string, args ...string) verbs.Result {
	t.Helper()
	if key, err := verbs.Key(cli.Root(), append(strings.Fields(verb), args...)); err == nil {
		env.Card = append(env.Card, key)
	}
	return verbs.Run(context.Background(), cli.Root(), verbs.Call{Verb: verb, Args: args, Stdin: stdin}, env)
}

// TestRunRefusesCallsOutsideTheCard (F-0323 C3): a confirmation authorizes the
// commands of the confirmed card and nothing else — same command in another
// spelling passes, any other command is not_in_card and never runs.
func TestRunRefusesCallsOutsideTheCard(t *testing.T) {
	card, err := verbs.Key(cli.Root(), []string{"reference", "new", "Ref A", "--url", "https://example.com/a"})
	if err != nil {
		t.Fatal(err)
	}
	env := verbs.Env{Bin: "/nonexistent", Confirmed: true, Card: []string{card}}
	call := func(verb string, args ...string) verbs.Result {
		return verbs.Run(context.Background(), cli.Root(), verbs.Call{Verb: verb, Args: args}, env)
	}
	if res := call("reference new", "--url=https://example.com/a", "Ref A"); !errors.Is(res.Err, verbs.ErrExecFailed) {
		t.Fatalf("the card's own command = %+v, want it to pass the gate (exec_failed on the bogus Bin)", res)
	}
	for _, c := range [][]string{
		{"reference new", "Ref B", "--url", "https://example.com/a"},
		{"reference new", "Ref A", "--url", "https://example.com/a", "--force"},
		{"note new", "Nota", "--facet", "process", "--component", "chat"},
	} {
		if res := call(c[0], c[1:]...); !errors.Is(res.Err, verbs.ErrNotInCard) || res.Error.Code != "not_in_card" {
			t.Fatalf("%v = %+v, want not_in_card", c, res)
		}
	}
	env.Card = nil // no card: fail closed
	if res := call("reference new", "Ref A", "--url", "https://example.com/a"); !errors.Is(res.Err, verbs.ErrNotInCard) {
		t.Fatalf("no card = %+v, want not_in_card", res)
	}
}

func TestRunCreatesAndReturnsID(t *testing.T) {
	env := newProject(t)
	env.Conversation, env.Turn = "c1", 3
	res := run(t, env, "reference new", "conteúdo", "Ref T", "--url", "https://example.com/t", "--facet", "code", "--component", "chat")
	if !res.OK || res.ID != "R-0001" || !slices.Equal(res.IDs, []string{"R-0001"}) || !strings.Contains(res.Output, "R-0001 registered") || res.Error != nil {
		t.Fatalf("Run = %+v, want OK with R-0001", res)
	}
	evs, err := store.ReadEvents(env.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	var created *domain.Event
	for i := range evs {
		if evs[i].Type == "reference.created" {
			created = &evs[i]
		}
	}
	if created == nil {
		t.Fatalf("no reference.created among %+v", evs)
	}
	p := created.Payload
	if p["origin"] != "chat" || p["conversation"] != "c1" || p["turn"] != "3" || !strings.HasPrefix(created.Actor, "agent:") {
		t.Fatalf("event = %+v, want origin=chat conversation=c1 turn=3 actor=agent:<user>@<host>", created)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(p["tool_call"]) {
		t.Fatalf("tool_call = %q, want 16 hex", p["tool_call"])
	}
}

func TestRunNamedErrors(t *testing.T) {
	env := newProject(t)
	lockEnv := env
	cases := []struct {
		name  string
		verb  string
		args  []string
		setup func(t *testing.T, e *verbs.Env)
		want  error
		code  string
	}{
		{"unknown verb", "feature fly", nil, nil, verbs.ErrUnknownVerb, "unknown_verb"},
		{"group verb", "room minutes", nil, nil, verbs.ErrUnknownVerb, "unknown_verb"},
		{"group with subverb in args", "room", []string{"minutes", "template"}, nil, verbs.ErrUnknownVerb, "unknown_verb"},
		{"missing title", "reference new", []string{"--url", "u"}, nil, verbs.ErrInvalidArguments, "invalid_arguments"},
		{"unknown flag", "reference new", []string{"T", "--url", "u", "--nope"}, nil, verbs.ErrInvalidArguments, "invalid_arguments"},
		{"dir flag", "reference new", []string{"T", "--url", "u", "--dir", "/tmp"}, nil, verbs.ErrInvalidArguments, "invalid_arguments"},
		{"file flag", "reference new", []string{"T", "--url", "u", "--file", "x"}, nil, verbs.ErrInvalidArguments, "invalid_arguments"},
		{"core refusal", "reference new", []string{"x", "--url="}, nil, verbs.ErrCommandFailed, "command_failed"},
		{"missing binary", "status", nil, func(t *testing.T, e *verbs.Env) { e.Bin = "/nonexistent/cerne" }, verbs.ErrExecFailed, "exec_failed"},
		{"timeout", "status", nil, func(t *testing.T, e *verbs.Env) {
			t.Setenv("CERNE_TEST_HANG", "1")
			e.Timeout = 200 * time.Millisecond
		}, verbs.ErrTimeout, "timeout"},
		{"lock conflict", "reference new", []string{"Lock", "--url", "https://example.com/l"}, func(t *testing.T, e *verbs.Env) {
			lock := gitback.LockPath(lockEnv.RepoDir)
			if err := os.WriteFile(lock, []byte(fmt.Sprintf("%d\n%d\n", 999999, time.Now().UnixNano())), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(lock) })
			e.Timeout = 500 * time.Millisecond
		}, verbs.ErrLockConflict, "lock_conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := env
			if tc.setup != nil {
				tc.setup(t, &e)
			} else {
				e.Bin = "/nonexistent/cerne" // gate refusals never reach exec
			}
			if tc.want == verbs.ErrCommandFailed {
				e.Bin = env.Bin
			}
			res := run(t, e, tc.verb, "", tc.args...)
			if res.OK || !errors.Is(res.Err, tc.want) || res.Error == nil || res.Error.Code != tc.code || res.Error.Message == "" {
				t.Fatalf("Run = %+v (err %v), want %s", res, res.Err, tc.code)
			}
			if tc.want == verbs.ErrTimeout && !strings.Contains(res.Error.Message, "lock") {
				t.Fatalf("timeout message = %q, want the backend-lock warning", res.Error.Message)
			}
			if tc.want == verbs.ErrCommandFailed && !strings.Contains(res.Error.Message, "url cannot be empty") {
				t.Fatalf("message = %q, want the core's own refusal", res.Error.Message)
			}
		})
	}
}

func TestRunDeniesLifecycleWithoutExec(t *testing.T) {
	var n int
	for _, v := range verbs.From(cli.Root()) {
		if !v.Lifecycle {
			continue
		}
		n++
		res := verbs.Run(context.Background(), cli.Root(), verbs.Call{Verb: v.Path, Args: []string{"F-0001"}},
			verbs.Env{Bin: "/nonexistent", Confirmed: true})
		if !errors.Is(res.Err, verbs.ErrLifecycleDenied) || res.Error == nil || res.Error.Code != "lifecycle_denied" {
			t.Errorf("%s: Run = %+v, want lifecycle_denied without exec", v.Path, res)
		}
	}
	if n == 0 {
		t.Fatal("no lifecycle verb in the catalog")
	}
}

// TestRunDeniesNonChatVerbs (F-0323 C4): an operational verb and a verb with no
// chat class never run through the chat — denied before confirmation, without
// exec.
func TestRunDeniesNonChatVerbs(t *testing.T) {
	env := verbs.Env{Bin: "/nonexistent", Confirmed: true}
	res := verbs.Run(context.Background(), cli.Root(), verbs.Call{Verb: "pull", Args: []string{"--force"}}, env)
	if !errors.Is(res.Err, verbs.ErrLocalOnly) || res.Error == nil || res.Error.Code != "local_only" {
		t.Fatalf("pull --force = %+v, want local_only", res)
	}
	root := cli.Root()
	root.AddCommand(&cobra.Command{Use: "zz-raw", RunE: func(*cobra.Command, []string) error { return nil }})
	res = verbs.Run(context.Background(), root, verbs.Call{Verb: "zz-raw"}, env)
	if !errors.Is(res.Err, verbs.ErrLocalOnly) {
		t.Fatalf("unclassified zz-raw = %+v, want local_only", res)
	}
}

func TestRunLifecycleLeavesStateUntouched(t *testing.T) {
	env := newProject(t)
	cerne(t, "feature", "new", "Alvo", "--context", "ctx", "--dir", env.ProjDir)
	res := run(t, env, "feature approve", "", "F-0001")
	if !errors.Is(res.Err, verbs.ErrLifecycleDenied) {
		t.Fatalf("Run = %+v, want lifecycle_denied", res)
	}
	f, err := store.LoadFeature(env.RepoDir, "F-0001")
	if err != nil || f.State != domain.StateCandidate {
		t.Fatalf("F-0001 = %+v (%v), want candidate", f, err)
	}
	evs, _ := store.ReadEvents(env.RepoDir)
	for _, ev := range evs {
		if ev.Type == "feature.approved" {
			t.Fatalf("feature.approved recorded: %+v", ev)
		}
	}
}

func TestRunRequiresConfirmation(t *testing.T) {
	env := verbs.Env{Bin: "/nonexistent"}
	res := verbs.Run(context.Background(), cli.Root(), verbs.Call{Verb: "reference new", Args: []string{"T", "--url", "u"}}, env)
	if !errors.Is(res.Err, verbs.ErrConfirmationRequired) || res.Error == nil || res.Error.Code != "confirmation_required" {
		t.Fatalf("unconfirmed write = %+v, want confirmation_required", res)
	}
	res = verbs.Run(context.Background(), cli.Root(), verbs.Call{Verb: "feature approve", Args: []string{"F-0001"}}, env)
	if !errors.Is(res.Err, verbs.ErrLifecycleDenied) {
		t.Fatalf("unconfirmed lifecycle = %+v, want lifecycle_denied (lifecycle wins)", res)
	}
}

func TestRunReadVerbHasNoID(t *testing.T) {
	env := newProject(t)
	res := run(t, env, "status", "")
	if !res.OK || res.ID != "" || res.Error != nil {
		t.Fatalf("Run(status) = %+v, want OK without id", res)
	}
}

// --help runs only after the whole gate (F-0323 C1): a help request is still
// validated like the verb itself, and a valued --help/-h — which the child
// would read as false and then run the verb — is refused.
func TestRunHelpOnlyAfterFullValidation(t *testing.T) {
	env := newProject(t)
	res := run(t, env, "reference new", "", "T", "--url", "u", "--help")
	if !res.OK || !strings.Contains(res.Output, "Usage:") || res.ID != "" {
		t.Fatalf("Run(reference new T --url u --help) = %+v, want OK with the usage", res)
	}
	for _, args := range [][]string{
		{"--help"}, // no title: validation comes first
		{"Nota vazada", "--facet", "process", "--component", "chat", "--help=false", "--file", "/etc/hosts"},
		{"Nota vazada", "--facet", "process", "--component", "chat", "--help=false"},
		{"Nota vazada", "-h=x"},
		{"Nota vazada", "-h=false", "--dir", "/tmp"},
	} {
		res := run(t, env, "note new", "", args...)
		if !errors.Is(res.Err, verbs.ErrInvalidArguments) {
			t.Fatalf("Run(note new %v) = %+v, want invalid_arguments", args, res)
		}
	}
	if notes, err := store.LoadAllNotes(env.RepoDir); err != nil || len(notes) != 0 {
		t.Fatalf("notes = %+v (%v), want none created", notes, err)
	}
}

// TestRunRecordsTheCallsModel (F-0375 crit. 6): a spec a chat tool call
// writes is the chat's [llm] call — its model reaches the child, which records
// it as llm-config, not as the server's environment.
func TestRunRecordsTheCallsModel(t *testing.T) {
	env := newProject(t)
	t.Setenv("CERNE_PROVIDER", "openai")
	t.Setenv("CERNE_MODEL", "gpt-server")
	env.Model = "claude-chosen"
	res := run(t, env, "epic new", "# Épico\n\n## Problema\n\nO chat escreveu esta spec.\n", "Épico do chat")
	if !res.OK {
		t.Fatalf("Run = %+v", res)
	}
	evs, err := store.ReadEvents(env.RepoDir)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]string
	for _, ev := range evs {
		if ev.Type == "feature.doc_edited" {
			p = ev.Payload
		}
	}
	if p["source"] != "llm-config" || p["model"] != "claude-chosen" || p["doc"] != "spec" {
		t.Fatalf("doc_edited = %v, want llm-config with the call's model claude-chosen", p)
	}
}
