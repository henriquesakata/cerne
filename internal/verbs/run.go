package verbs

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"gitlab.com/rockt/cerne/internal/gitback"
	"gitlab.com/rockt/cerne/internal/store"
)

// The named errors of the tool contract (F-0323 criterion 2): one sentinel per
// stable code, wrapped with %w, matched with errors.Is. codeOf maps them.
var (
	ErrUnknownVerb          = errors.New("unknown verb")
	ErrInvalidArguments     = errors.New("invalid arguments")
	ErrLifecycleDenied      = errors.New("lifecycle verb denied: lifecycle is a human act, hand the command to the human to run in a terminal")
	ErrLocalOnly            = errors.New("verb not offered in the chat: operational or unclassified verbs run only in the human's terminal")
	ErrConfirmationRequired = errors.New("confirmation required: the human has not confirmed this turn's command card")
	ErrNotInCard            = errors.New("command not in the confirmed card: the confirmation covers only the commands the human saw")
	ErrCommandFailed        = errors.New("command failed")
	ErrLockConflict         = errors.New("backend lock held by another process")
	ErrTimeout              = errors.New("command timed out")
	ErrExecFailed           = errors.New("could not start cerne")
)

var codes = []struct {
	err  error
	code string
}{
	{ErrUnknownVerb, "unknown_verb"},
	{ErrInvalidArguments, "invalid_arguments"},
	{ErrLifecycleDenied, "lifecycle_denied"},
	{ErrLocalOnly, "local_only"},
	{ErrConfirmationRequired, "confirmation_required"},
	{ErrNotInCard, "not_in_card"},
	{ErrCommandFailed, "command_failed"},
	{ErrLockConflict, "lock_conflict"},
	{ErrTimeout, "timeout"},
	{ErrExecFailed, "exec_failed"},
}

func codeOf(err error) string {
	for _, c := range codes {
		if errors.Is(err, c.err) {
			return c.code
		}
	}
	return "exec_failed" // unreachable: Run only fails with the sentinels above
}

const (
	defaultTimeout = 60 * time.Second // per call, below the backend lock's 150 s acquire
	maxOutput      = 16 << 10
	maxMessage     = 2 << 10
)

// Call is one tool invocation: a catalog verb, its argv and optional stdin
// (the content a --file would carry).
type Call struct {
	Verb  string   `json:"verb"`
	Args  []string `json:"args"`
	Stdin string   `json:"stdin,omitempty"`
}

// Env is the turn a call runs in.
type Env struct {
	Bin          string        // the cerne binary (os.Executable() in production)
	ProjDir      string        // the child's cwd: the chat's project
	RepoDir      string        // the backend clone: read for the call's events and lock
	Confirmed    bool          // the human confirmed this turn's command card
	Card         []string      // Key of each command of that card: the only calls allowed (nil allows none)
	Conversation string        // provenance stamped on every event the call writes
	Turn         int           // index of the confirmed card message
	Timeout      time.Duration // per call; 0 → defaultTimeout
}

// Error is the named failure of a Result.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Result is the structured tool result (frozen contract): success carries the
// affected id from the event log, failure carries a named error, never empty.
type Result struct {
	OK     bool     `json:"ok"`
	ID     string   `json:"id,omitempty"`  // the primary entity: the first one the call wrote
	IDs    []string `json:"ids,omitempty"` // every entity the call wrote (C10)
	Output string   `json:"output"`
	Error  *Error   `json:"error,omitempty"`
	Err    error    `json:"-"`
}

// Fail is the Result of a call refused before Run could see it (a tool input
// that does not decode): err wraps one of the sentinels, and the code comes
// from the same mapping Run uses.
func Fail(err error) Result { return fail(err, "") }

func fail(err error, out string) Result {
	return Result{Output: out, Error: &Error{Code: codeOf(err), Message: err.Error()}, Err: err}
}

// Run executes call under the chat gate. Order: resolve what cobra would run
// (unknown_verb), lifecycle (denied before anything, confirmed or not), any
// class but chat (local_only — unclassified fails closed),
// confirmation, cobra validation on root (invalid_arguments), the confirmed
// card (not_in_card: only its commands run), then the cerne
// binary as a child with CERNE_ACTOR=agent and the provenance env. Run parses
// flags into root: pass a fresh tree per call.
func Run(ctx context.Context, root *cobra.Command, call Call, env Env) Result {
	argv := append(strings.Fields(call.Verb), call.Args...)
	cmd, rest, err := root.Find(argv)
	want := strings.Join(append([]string{root.Name()}, strings.Fields(call.Verb)...), " ")
	if err != nil || cmd == root || cmd.CommandPath() != want || !cmd.Runnable() || hidden(cmd) {
		return fail(fmt.Errorf("%w: %q (want a verb of the catalog)", ErrUnknownVerb, call.Verb), "")
	}
	switch classOf(cmd) {
	case ClassLifecycle:
		return fail(fmt.Errorf("%w: cerne %s", ErrLifecycleDenied, call.Verb), "")
	case ClassChat:
	default:
		return fail(fmt.Errorf("%w: cerne %s", ErrLocalOnly, call.Verb), "")
	}
	if !env.Confirmed {
		return fail(ErrConfirmationRequired, "")
	}
	if err := validate(cmd, rest); err != nil {
		return fail(fmt.Errorf("%w: %v", ErrInvalidArguments, err), "")
	}
	if !slices.Contains(env.Card, keyOf(cmd)) {
		return fail(fmt.Errorf("%w: cerne %s %s", ErrNotInCard, call.Verb, strings.Join(call.Args, " ")), "")
	}
	return execute(ctx, argv, call.Stdin, env)
}

// Key is the canonical form of a command line (argv without "cerne"): the
// resolved command, its positionals and its set flags with their values, so
// `--url u` and `--url=u`, or flags in another order, are the same command
// (C3). The server keys the confirmed card's lines with it; Run keys each call.
func Key(root *cobra.Command, argv []string) (string, error) {
	cmd, rest, err := root.Find(argv)
	if err != nil || cmd == root {
		return "", fmt.Errorf("%w: %q", ErrUnknownVerb, argv)
	}
	cmd.InitDefaultHelpFlag()
	if err := cmd.ParseFlags(rest); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	return keyOf(cmd), nil
}

// keyOf keys a command whose flags are already parsed.
func keyOf(cmd *cobra.Command) string {
	var flags []string
	cmd.Flags().Visit(func(f *pflag.Flag) { flags = append(flags, f.Name+"="+f.Value.String()) }) // set flags, sorted
	return strings.Join([]string{cmd.CommandPath(), strings.Join(cmd.Flags().Args(), "\x1f"), strings.Join(flags, "\x1f")}, "\x00")
}

func hidden(c *cobra.Command) bool {
	for ; c != nil; c = c.Parent() {
		if c.Hidden {
			return true
		}
	}
	return false
}

// validate runs cobra's own checks, plus the trust-boundary refusals: the
// project is the chat's (--dir) and content comes through stdin (--file would
// read an arbitrary local file into the shared backend). A help request runs
// only after the whole gate passed, and only as a bare --help/-h: a valued one
// (--help=false) would reach the child as "no help" and run the verb.
func validate(cmd *cobra.Command, rest []string) error {
	cmd.InitDefaultHelpFlag()
	if err := cmd.ParseFlags(rest); err != nil {
		return err
	}
	help := cmd.Flags().Lookup("help")
	if help.Changed && help.Value.String() != "true" {
		return errors.New("--help takes no value")
	}
	for _, f := range []string{"dir", "file"} {
		if cmd.Flags().Changed(f) {
			return fmt.Errorf("--%s is not allowed (the project is the chat's; pass content as stdin)", f)
		}
	}
	if err := cmd.ValidateArgs(cmd.Flags().Args()); err != nil {
		return err
	}
	if err := cmd.ValidateRequiredFlags(); err != nil {
		return err
	}
	return cmd.ValidateFlagGroups()
}

func execute(ctx context.Context, argv []string, stdin string, env Env) Result {
	timeout := env.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	nonce := make([]byte, 8)
	rand.Read(nonce) // never fails per crypto/rand docs
	call := hex.EncodeToString(nonce)

	c := exec.CommandContext(ctx, env.Bin, argv...)
	c.Dir = env.ProjDir
	c.Env = append(os.Environ(),
		"CERNE_ACTOR=agent",
		"CERNE_ORIGIN=chat",
		"CERNE_CONVERSATION="+env.Conversation,
		"CERNE_TURN="+strconv.Itoa(env.Turn),
		"CERNE_TOOL_CALL="+call,
	)
	if stdin != "" {
		c.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	// C12: on the deadline the child is interrupted first and killed only
	// after WaitDelay, so a cerne mid-write gets its chance to finish.
	c.Cancel = func() error { return c.Process.Signal(os.Interrupt) }
	c.WaitDelay = 5 * time.Second
	if err := c.Start(); err != nil {
		return fail(fmt.Errorf("%w: %v", ErrExecFailed, err), "")
	}
	waitErr := c.Wait()
	out := truncate(stdout.String(), maxOutput)
	ids := affectedIDs(env.RepoDir, call)
	var res Result
	switch {
	case ctx.Err() != nil && lockHeldByOther(env.RepoDir, c.Process.Pid):
		res = fail(fmt.Errorf("%w after %s: retry once the other cerne operation finishes", ErrLockConflict, timeout), out)
	case ctx.Err() != nil:
		res = fail(fmt.Errorf("%w after %s: command interrupted; the backend lock may still be held (stolen after 2 min) — check `cerne status` before retrying", ErrTimeout, timeout), out)
	case waitErr != nil:
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		res = fail(fmt.Errorf("%w: %s", ErrCommandFailed, truncate(msg, maxMessage)), out)
	default:
		res = Result{OK: true, Output: out}
	}
	// Filled even on failure: a partial write still names its entities.
	if res.IDs = ids; len(ids) > 0 {
		res.ID = ids[0]
	}
	return res
}

// affectedIDs are the entities of every event the call wrote, in order and
// once each, found by the call's nonce (stamped by store.AppendEvent): ids come
// from the log, never from the command's prose. Read-only verbs write nothing.
// ponytail: reads the whole log per call; scan only the current month's file if it grows slow.
func affectedIDs(repoDir, call string) []string {
	if repoDir == "" {
		return nil
	}
	evs, err := store.ReadAllEvents(repoDir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, ev := range evs {
		if ev.Payload["tool_call"] == call && !slices.Contains(ids, ev.EntityID) {
			ids = append(ids, ev.EntityID)
		}
	}
	return ids
}

// lockHeldByOther reports a backend lockfile whose holder is not the child.
func lockHeldByOther(repoDir string, child int) bool {
	if repoDir == "" {
		return false
	}
	data, err := os.ReadFile(gitback.LockPath(repoDir))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.SplitN(string(data), "\n", 2)[0])
	return err == nil && pid != child
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n… [truncated]"
}
