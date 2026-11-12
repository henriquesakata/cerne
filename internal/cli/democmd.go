package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/go-git/go-git/v5"
	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/examples/demo"
	"github.com/henriquesakata/cerne/internal/gitback"
)

var (
	errDemoExists        = errors.New("already exists; remove it or pass another directory")
	errDemoInsideProject = errors.New("would sit inside the cerne project at")
	errDemoInterrupted   = errors.New("interrupted")
)

// demoCancelled is set by the interrupt handler; run checks it between steps.
var demoCancelled atomic.Bool

// demoRun, when set, replaces cerneInProcess so a test can fail one step.
var demoRun func(body string, args ...string) (string, error)

// cerneInProcess runs one cerne command in-process — the same commands, and so
// the same internal/app layer, a user types — with body as its stdin.
func cerneInProcess(body string, args ...string) (string, error) {
	root := Root()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(body))
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func newDemoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "demo [<dir>]",
		Short: "Create taskflow, an example project with a realistic history, in <dir> (default ./cerne-demo)",
		Long: `Creates <dir>/taskflow (the project) and <dir>/taskflow-docs.git (a local
bare backend) from documents embedded in the binary: no clone, no shell, no
network. <dir> must not exist and must sit outside any cerne project. If a
step fails, everything the command created is removed.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := "cerne-demo"
			if len(args) == 1 {
				dest = args[0]
			}
			return runDemo(cmd.OutOrStdout(), dest)
		},
	}
}

// demoBuild ports examples/demo/seed.sh step by step. The first failing step
// sticks in err and every later call is a no-op, so the sequence reads like
// the script.
type demoBuild struct {
	w    io.Writer
	proj string // absolute path of the project
	out  string // what the last command printed
	err  error
}

func (d *demoBuild) run(body string, args ...string) {
	if d.err == nil && demoCancelled.Load() {
		d.err = errDemoInterrupted
	}
	if d.err != nil {
		return
	}
	run := cerneInProcess
	if demoRun != nil {
		run = demoRun
	}
	out, err := run(body, args...)
	d.out = out
	if err != nil {
		d.err = fmt.Errorf("step 'cerne %s' failed: %w\n%s", strings.Join(args, " "), err, strings.TrimRight(out, "\n"))
	}
}

// content reads one embedded document.
func (d *demoBuild) content(path string) string {
	if d.err != nil {
		return ""
	}
	data, err := fs.ReadFile(demo.Content, "content/"+path)
	if err != nil {
		d.err = fmt.Errorf("missing content file: %w", err)
	}
	return string(data)
}

// id is the id of that kind the last command printed (F-0001, D-0002, …).
// Asking for the kind keeps a related entity named in the same output from
// being taken for it.
func (d *demoBuild) id(prefix string) string {
	if d.err != nil {
		return ""
	}
	id := regexp.MustCompile(prefix + `-[0-9]{4}`).FindString(d.out)
	if id == "" {
		d.err = fmt.Errorf("the last cerne command printed no %s- id:\n%s", prefix, d.out)
	}
	return id
}

// put copies a plan or result into the slot `cerne pull` materialized.
func (d *demoBuild) put(id, slug, doc string) {
	body := d.content("features/" + slug + "/" + doc + ".md")
	if d.err != nil {
		return
	}
	slots, _ := filepath.Glob(filepath.Join(d.proj, ".docs", "current", "features", id+"-*"))
	if len(slots) == 0 {
		d.err = fmt.Errorf("cerne pull did not materialize a folder for %s", id)
		return
	}
	d.err = os.WriteFile(filepath.Join(slots[len(slots)-1], doc+".md"),
		[]byte("---\ncerne_id: "+id+"\n---\n\n"+body), 0o644)
}

// feature walks one feature through the lifecycle up to target.
func (d *demoBuild) feature(slug, title, target string, flags ...string) string {
	d.run(d.content("features/"+slug+"/spec.md"), append([]string{"feature", "new", title}, flags...)...)
	id := d.id("F")
	if target != "candidate" && target != "rejected" {
		d.run("", "feature", "approve", id)
	}
	if target == "in_development" || target == "implemented" || target == "done" {
		d.run("", "pull")
		d.put(id, slug, "plan")
		d.run("", "push")
		d.run("", "feature", "start", id)
	}
	if target == "implemented" || target == "done" {
		d.run("", "pull")
		d.put(id, slug, "result")
		d.run("", "push")
		d.run("", "feature", "complete", id)
	}
	d.say("  %s  %-15s %s", id, target, title)
	return id
}

func (d *demoBuild) say(format string, a ...any) {
	if d.err == nil {
		fmt.Fprintf(d.w, format+"\n", a...)
	}
}

// gates sets who may cross each gate ("auto" or "human"), under the [gates]
// table init may already have written (F-0364), never a second header.
func (d *demoBuild) gates(rc, approve, accept string) {
	if d.err != nil {
		return
	}
	keys := "approve = \"" + approve + "\"\naccept = \"" + accept + "\"\n"
	var b strings.Builder
	done := false
	for _, line := range strings.SplitAfter(rc, "\n") {
		b.WriteString(line)
		if strings.TrimRight(line, "\r\n") == "[gates]" {
			b.WriteString(keys)
			done = true
		}
	}
	if !done {
		b.WriteString("\n[gates]\n" + keys)
	}
	d.err = os.WriteFile(filepath.Join(d.proj, ".cernerc"), []byte(b.String()), 0o644)
}

func runDemo(w io.Writer, dest string) (err error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	// missing: the components of abs that do not exist yet, deepest first.
	var missing []string
	have := abs
	for {
		if _, err := os.Lstat(have); err == nil {
			break
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		missing = append(missing, have)
		if filepath.Dir(have) == have {
			break
		}
		have = filepath.Dir(have)
	}
	if len(missing) == 0 {
		return fmt.Errorf("%s %w", dest, errDemoExists)
	}
	// cerne finds its project by walking up, so a demo inside another cerne
	// project would be taken for part of it. Walk from the real path of the
	// deepest existing parent (a symlink may lead into a project), and refuse
	// before creating anything.
	real, err := filepath.EvalSymlinks(have)
	if err != nil {
		return err
	}
	for up := real; ; up = filepath.Dir(up) {
		if _, err := os.Stat(filepath.Join(up, ".cernerc")); err == nil {
			home, _ := os.UserHomeDir()
			return fmt.Errorf("%s %w %s; pass a directory outside it, for example: cerne demo \"%s\"",
				dest, errDemoInsideProject, up, filepath.Join(home, "cerne-demo"))
		}
		if filepath.Dir(up) == up {
			break
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	// From the first directory created on, cleanup removes what the command
	// created — never a directory that was there before — on any failure,
	// panic or interrupt, so a half-built demo (gates open) is never left.
	created, succeeded := "", false
	defer func() {
		_ = os.Chdir(wd) // Windows cannot remove the working directory
		if succeeded || created == "" {
			return
		}
		what := "removed " + created
		if rerr := os.RemoveAll(created); rerr != nil {
			what = fmt.Sprintf("could not remove %s: %v", created, rerr)
		}
		startWriteNotice() // the project is gone: no "wrote to" line after the error
		if err != nil {
			err = fmt.Errorf("cerne demo stopped before finishing; %s\n%w", what, err)
		}
	}()
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil {
			if i == 0 && errors.Is(err, fs.ErrExist) { // raced in since the check
				return fmt.Errorf("%s %w", dest, errDemoExists)
			}
			return err
		}
		if created == "" {
			created = missing[i]
		}
	}

	// An interrupt only flags the build: the step in flight finishes, run
	// stops before the next one, and cleanup happens here, on this goroutine.
	// Nothing but this goroutine ever moves the process cwd.
	demoCancelled.Store(false)
	sig, done := make(chan os.Signal, 1), make(chan struct{})
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer func() { signal.Stop(sig); close(done) }()
	go func() {
		select {
		case <-sig:
			demoCancelled.Store(true)
		case <-done:
		}
	}()

	// Every user-level dir a step writes through points inside <dir> while
	// building, so nothing reaches your real projects; CERNE_ROOT must not
	// redirect a step to another project. All restored on every path.
	for k, v := range map[string]string{
		"CERNE_CACHE_DIR":  filepath.Join(abs, ".cache"),
		"CERNE_DATA_DIR":   filepath.Join(abs, ".data"),
		"CERNE_CONFIG_DIR": filepath.Join(abs, ".config"),
		"CERNE_ROOT":       "",
	} {
		old, had := os.LookupEnv(k)
		defer func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		}()
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}

	fmt.Fprintf(w, "Creating the taskflow demo in %s\n", abs)
	backend := filepath.Join(abs, "taskflow-docs.git")
	proj := filepath.Join(abs, "taskflow")
	if err := gitback.InitBare(backend); err != nil {
		return fmt.Errorf("creating the backend: %w", err)
	}
	if _, err := git.PlainInit(proj, false); err != nil {
		return fmt.Errorf("creating the project repository: %w", err)
	}
	// Steps find the project from the cwd (most commands have no --dir), so
	// it is set here once, before any step, and restored by the defer above.
	if err := os.Chdir(proj); err != nil {
		return err
	}
	d := &demoBuild{w: w, proj: proj}
	d.run("", "init", "--backend", backend, "--name", "taskflow", "--docs-language", "en")
	if d.err != nil {
		return d.err
	}

	// In a real project, approving a spec and accepting a delivery are human
	// acts: an AI agent cannot sign off its own work. While building, the
	// gates are open so it runs unattended (even under an agent), and the
	// default configuration comes back before it finishes.
	rc, err := os.ReadFile(filepath.Join(proj, ".cernerc"))
	if err != nil {
		return err
	}
	d.say("Features:")
	d.gates(string(rc), "auto", "auto") // with accept on auto, completing lands on done
	google := d.feature("sign-in-with-google", "Sign in with Google", "done", "--facet", "auth", "--component", "oauth,login")
	facebook := d.feature("sign-in-with-facebook", "Sign in with Facebook", "rejected", "--facet", "auth", "--component", "oauth,login")
	d.run("", "feature", "reject", facebook, "--reason", "Measured first: 1.8% of new sign-ups asked for it, against 58% using Google.")
	d.feature("export-tasks-as-csv", "Export tasks as CSV", "done", "--facet", "data", "--component", "export,api")
	d.gates(string(rc), "auto", "human") // from here on, a completed feature waits for a human accept
	epic := d.feature("notifications", "Notifications", "in_development", "--facet", "code", "--component", "notify")
	digest := d.feature("weekly-email-digest", "Weekly email digest", "implemented", "--parent", epic, "--facet", "code", "--component", "notify,email")
	d.feature("slack-notifications", "Slack notifications", "approved", "--parent", epic, "--facet", "code", "--component", "notify,slack")
	d.feature("recurring-tasks", "Recurring tasks", "in_development", "--facet", "code", "--component", "tasks,recurrence")
	d.feature("offline-mode", "Offline mode", "candidate", "--facet", "code", "--component", "web,sync")

	d.say("Decisions, knowledge and sources:")
	d.run(d.content("decisions/session-cookies.md"), "decision", "new", "Use server-side session cookies for auth", "--facet", "auth", "--component", "session")
	cookies := d.id("D")
	d.run(d.content("decisions/jwt-tokens.md"), "decision", "new", "Use JWT access tokens with rotating refresh tokens", "--facet", "auth", "--component", "tokens")
	jwt := d.id("D")
	d.run("", "decision", "substituir", cookies, "--by", jwt, "--reason", "The public API and the mobile client cannot use cookies.")
	d.say("  %s  superseded by %s", cookies, jwt)
	d.run(d.content("decisions/postgres.md"), "decision", "new", "Use Postgres as the primary store", "--facet", "data", "--component", "postgres")
	d.say("  %s  active decision", d.id("D"))
	d.run(d.content("notes/oauth-apex-domain.md"), "note", "new", "Google OAuth callback must stay on the apex domain", "--informs", jwt, "--facet", "auth", "--component", "oauth")
	d.say("  %s  note", d.id("N"))
	d.run(d.content("runbooks/rotate-jwt-signing-key.md"), "runbook", "new", "Rotate the JWT signing key")
	runbook := d.id("RB")
	d.run("", "runbook", "retag", runbook, "--facet", "auth", "--component", "tokens")
	d.say("  %s  runbook", runbook)
	d.run(d.content("references/rfc6749.md"), "reference", "new", "RFC 6749: The OAuth 2.0 Authorization Framework",
		"--url", "https://datatracker.ietf.org/doc/html/rfc6749", "--informs", google, "--facet", "auth", "--component", "oauth")
	d.say("  %s  external reference", d.id("R"))

	if d.err == nil {
		d.err = os.WriteFile(filepath.Join(proj, ".cernerc"), rc, 0o644) // back to the default: both gates human
	}
	d.run("", "pull")
	if d.err != nil {
		return d.err
	}
	succeeded = true

	fmt.Fprintf(w, `
Done. Explore it from %[1]s:

  cerne status               # features by state
  cerne ui                   # dashboard and kanban at http://localhost:7717
  cat .docs/CONTEXT.md       # what an AI agent reads: only the current state
  cerne search facebook      # the rejected proposal is still one search away
  cerne savings              # how much history stays out of the agent's context
  cerne feature accept %[2]s   # the weekly digest waits for you: accepting is a human act

To start over, delete "%[3]s".

cd "%[1]s" && cerne ui
`, proj, digest, abs)
	return nil
}
