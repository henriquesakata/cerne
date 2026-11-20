package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/selfupdate"
	"github.com/henriquesakata/cerne/internal/server"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/version"
)

// Seams for tests: the running executable and the Homebrew command.
var (
	updateExecutable = os.Executable
	brewCommand      = "brew"
)

// The panel's Update button (F-0480) swaps the binary with this file's own
// code; the server package cannot import this one, so it is injected here,
// where the version check lives (F-0476 crit. 11).
func init() {
	// F-0480: the panel's Update button swaps the binary with cerne update's
	// own code, which lives here.
	server.PlanBinary = func(exe string) (bool, string, string) {
		resolved, why, fix := binaryRefusal(exe)
		if why == "" && selfupdate.Detect(exe, resolved, version.Version, selfupdate.GoBins()) == selfupdate.Script {
			why, fix = writableRefusal(resolved)
		}
		return why == "", why, fix
	}
	server.ReplaceBinary = func(w io.Writer, exe string) server.BinaryOutcome {
		p, newExe := replaceBinary(w, w, exe)
		return server.BinaryOutcome{State: p.state, Detail: p.detail, Fix: p.fix, NewExe: newExe}
	}
}

// errUpdateNotDone is the non-zero exit of an update that left a part not
// done (F-0476 crit. 2); the parts already said why.
var errUpdateNotDone = errors.New("cerne update: not everything was updated (see the lines above)")

const brewFormula = "henriquesakata/tap/cerne"

// part is one line of the update's closing summary.
type part struct {
	name   string // binary, hooks, guide
	state  string // updated, up to date, not done, would update
	detail string // what changed, or why not
	fix    string // the command to run, for not done
}

const (
	stUpdated = "updated"
	stCurrent = "up to date"
	stNotDone = "not done"
	stWould   = "would update"
)

func (p part) String() string {
	s := p.name + ": " + p.state
	if p.detail != "" {
		s += " — " + p.detail
	}
	if p.fix != "" {
		s += " — run: " + p.fix
	}
	return s
}

func newUpdateCmd() *cobra.Command {
	var dir string
	var check, filesOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the cerne binary and this project's cerne files (hooks, guide) in one command",
		Long: `Updates the cerne binary to the latest release, then renews the hooks and the
authoring guide of the agents already connected to this project — the same as
'cerne hook install' followed by 'cerne skill install', connecting no new agent.

The binary is replaced the way it was installed: through Homebrew when it lives
under a Homebrew Cellar, otherwise by downloading the latest release, checking
it against the release's SHA256SUMS and swapping the executable in place. A
local build, a 'go install' or a folder you cannot write is left alone, with the
command to run. An agent never replaces the binary: that part is the human's.

It ends with one line per part (binary, hooks, guide): updated, up to date, or
not done with the reason and the command. The exit code is 0 only when nothing
was left not done. This is the only cerne command that asks the network for a
new version, and only when you run it. Outside a project it updates the binary
alone.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd, dir, check, filesOnly)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	cmd.Flags().BoolVar(&check, "check", false, "only report which project files would be renewed: writes nothing, uses no network, exits non-zero when something is out of date")
	// Also how the new binary renews the files after a swap (crit. 8).
	cmd.Flags().BoolVar(&filesOnly, "files-only", false, "renew the project files only, leaving the binary alone")
	return cmd
}

func runUpdate(cmd *cobra.Command, dir string, check, filesOnly bool) error {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		dir = wd
	}
	_, projDir, cfgErr := project.LoadConfig(dir)
	inProject := cfgErr == nil
	// A .cernerc that does not load: the binary is still updated (a newer one
	// may read it), and the project parts close as not done.
	brokenProject := cfgErr != nil && !errors.Is(cfgErr, project.ErrNoProject)
	out := cmd.OutOrStdout()
	if check {
		if !inProject {
			return fmt.Errorf("cerne update --check looks at a project's files: %w", cfgErr)
		}
		return updateCheck(out, projDir)
	}

	notDone := false
	if !filesOnly {
		bin, newExe := updateBinary(cmd)
		fmt.Fprintln(out, bin)
		notDone = bin.state == stNotDone
		if newExe != "" && inProject {
			return reexecFiles(cmd, newExe, projDir)
		}
	}
	switch {
	case brokenProject:
		for _, name := range []string{"hooks", "guide"} {
			fmt.Fprintln(out, part{name: name, state: stNotDone, detail: "the project's .cernerc does not load: " + cfgErr.Error(), fix: "cerne update, after fixing .cernerc"})
		}
		notDone = true
	case !inProject:
		fmt.Fprintln(out, "no cerne project here (no .cernerc from "+dir+" up): only the binary was looked at, no project was touched")
	default:
		hookPart, guidePart := renewHooks(projDir), renewGuide(projDir)
		fmt.Fprintln(out, hookPart)
		fmt.Fprintln(out, guidePart)
		if hookPart.state == stUpdated {
			fmt.Fprintln(out, "The hooks apply from the agent's next session; local edits to managed hooks are not kept (to turn the hooks off for a process, set CERNE_NO_AGENT_HOOKS=1).")
		}
		notDone = notDone || hookPart.state == stNotDone || guidePart.state == stNotDone
	}
	if notDone {
		return errUpdateNotDone
	}
	return nil
}

// updateCheck reports the project files older than this build (crit. 3).
func updateCheck(out io.Writer, projDir string) error {
	h, g := staleManaged(projDir)
	parts := []part{{name: "hooks", state: stCurrent}, {name: "guide", state: stCurrent}}
	if len(h) > 0 {
		parts[0] = part{name: "hooks", state: stWould, detail: strings.Join(h, ", ")}
	}
	if len(g) > 0 {
		parts[1] = part{name: "guide", state: stWould, detail: strings.Join(g, ", ")}
	}
	fmt.Fprintln(out, "binary: not checked (--check uses no network)")
	for _, p := range parts {
		fmt.Fprintln(out, p)
	}
	if len(h)+len(g) == 0 {
		return nil
	}
	return fmt.Errorf("project files out of date with this cerne build — run 'cerne update'")
}

// staleManaged names the project's managed hooks and guide files older than
// this build, without the plugin's absent copies (F-0454): the one verdict of
// cerne next, cerne update --check and the session start.
func staleManaged(projDir string) (hookNames, guideNames []string) {
	return withoutPluginAbsent(projDir, hooks.Stale(projDir)), withoutPluginAbsent(projDir, skills.Stale(projDir))
}

// defaultHookProviders is the agent set `cerne hook install` uses with no
// --provider: Claude Code plus every provider whose config dir exists.
func defaultHookProviders(dir string) []hooks.Provider {
	return append([]hooks.Provider{hooks.ProviderClaude}, hooks.DetectProviders(dir)...)
}

func renewHooks(projDir string) part {
	results, err := hooks.Install(projDir, defaultHookProviders(projDir)...)
	if err != nil {
		return part{name: "hooks", state: stNotDone, detail: err.Error(), fix: "cerne hook install"}
	}
	if left, _ := staleManaged(projDir); len(left) > 0 {
		return part{name: "hooks", state: stNotDone, detail: "still out of date: " + strings.Join(left, ", "), fix: "cerne hook install"}
	}
	var changed []string
	for _, r := range results {
		if r.Status == hooks.StatusCreated || r.Status == hooks.StatusUpdated {
			changed = append(changed, r.Asset)
		}
	}
	if len(changed) == 0 {
		return part{name: "hooks", state: stCurrent}
	}
	return part{name: "hooks", state: stUpdated, detail: strings.Join(changed, ", ")}
}

func renewGuide(projDir string) part {
	results, err := skills.Install(projDir, hooks.DetectProviders(projDir)...)
	if err != nil {
		return part{name: "guide", state: stNotDone, detail: err.Error(), fix: "cerne skill install"}
	}
	if _, left := staleManaged(projDir); len(left) > 0 {
		return part{name: "guide", state: stNotDone, detail: "still out of date: " + strings.Join(left, ", "), fix: "cerne skill install"}
	}
	var changed []string
	for _, r := range results {
		if r.Status == skills.StatusCreated || r.Status == skills.StatusUpdated {
			name := r.Path
			if name == "" {
				name = r.Skill
			}
			changed = append(changed, name)
		}
	}
	if len(changed) == 0 {
		return part{name: "guide", state: stCurrent}
	}
	return part{name: "guide", state: stUpdated, detail: strings.Join(changed, ", ")}
}

// installScriptCmd is the install script line for this OS: the fix when the
// binary cannot be replaced from inside.
func installScriptCmd() string {
	if runtime.GOOS == "windows" {
		return "irm https://raw.githubusercontent.com/henriquesakata/cerne/main/install.ps1 | iex"
	}
	return "curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh"
}

// updateBinary brings the binary to the latest release the way it was
// installed, and returns its line plus the new executable when it changed.
// An agent never does it (crit. 10): the binary belongs to the machine.
func updateBinary(cmd *cobra.Command) (part, string) {
	if app.AgentEnv() { // crit. 10
		return part{name: "binary", state: stNotDone, detail: "replacing the binary is the human's part: it belongs to the machine, not to the project", fix: "cerne update, in your own terminal"}, ""
	}
	exe, err := updateExecutable()
	if err != nil {
		return part{name: "binary", state: stNotDone, detail: "cannot locate the running executable: " + err.Error(), fix: installScriptCmd()}, ""
	}
	return replaceBinary(cmd.OutOrStdout(), cmd.ErrOrStderr(), exe)
}

// binaryRefusal says why exe cannot be replaced from inside, and the command
// that replaces it, without the network; both empty when it can be. A folder
// that cannot be written refuses only a script install: Homebrew writes its
// own.
func binaryRefusal(exe string) (resolved, why, fix string) {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	switch selfupdate.Detect(exe, resolved, version.Version, selfupdate.GoBins()) {
	case selfupdate.Dev:
		return resolved, "this is a local build (version dev), not a release: it is not replaced", `go build -ldflags="-s -w" -o ` + exe + " ./cmd/cerne, in your cerne clone"
	case selfupdate.GoInstall:
		return resolved, "installed by go install (" + filepath.Dir(exe) + "): go install replaces it", "go install github.com/henriquesakata/cerne/cmd/cerne@latest"
	}
	return resolved, "", ""
}

// writableRefusal is the script install's check on the folder it writes.
func writableRefusal(resolved string) (why, fix string) {
	dir := filepath.Dir(resolved)
	if err := selfupdate.Writable(dir); err != nil {
		return "cannot write " + dir + " to replace " + resolved + ": " + err.Error(), installScriptCmd()
	}
	return "", ""
}

// replaceBinary is the swap itself, for cerne update and for the panel's
// Update button (F-0480): Homebrew through Homebrew, a script install by the
// verified download. out and errOut receive Homebrew's own output.
func replaceBinary(out, errOut io.Writer, exe string) (part, string) {
	notDone := func(why, fix string) (part, string) {
		return part{name: "binary", state: stNotDone, detail: why, fix: fix}, ""
	}
	resolved, why, fix := binaryRefusal(exe)
	if why != "" {
		return notDone(why, fix)
	}
	cur := version.Version
	if selfupdate.Detect(exe, resolved, cur, selfupdate.GoBins()) == selfupdate.Homebrew {
		return brewUpgrade(out, errOut, resolved)
	}

	base := os.Getenv("CERNE_BASE_URL")
	if base == "" {
		base = selfupdate.DefaultBaseURL
	}
	latest, err := selfupdate.Latest(base)
	if err != nil {
		return notDone("could not resolve the latest release: "+err.Error(), installScriptCmd())
	}
	if !selfupdate.Newer(latest, cur) {
		detail := cur + " is the latest release"
		if cur != latest {
			detail = cur + " is not older than the latest release, " + latest
		}
		return part{name: "binary", state: stCurrent, detail: detail}, ""
	}
	if why, fix := writableRefusal(resolved); why != "" {
		return notDone(why, fix)
	}
	data, err := selfupdate.Fetch(base, latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return notDone(err.Error()+"; the old executable is untouched", installScriptCmd())
	}
	if err := selfupdate.Replace(resolved, data, runtime.GOOS); err != nil {
		return notDone("could not replace "+resolved+": "+err.Error()+"; the old executable is kept", installScriptCmd())
	}
	return part{name: "binary", state: stUpdated, detail: cur + " → " + latest + " (" + resolved + ")"}, resolved
}

// brewUpgrade leaves the replacement to Homebrew, so its records stay right
// (crit. 6); the binary changed when <prefix>/bin/cerne now points elsewhere.
func brewUpgrade(out, errOut io.Writer, resolved string) (part, string) {
	c := exec.Command(brewCommand, "upgrade", brewFormula)
	c.Stdout, c.Stderr = out, errOut
	if err := c.Run(); err != nil {
		return part{name: "binary", state: stNotDone, detail: "brew upgrade failed: " + err.Error(), fix: "brew upgrade " + brewFormula}, ""
	}
	link := selfupdate.BrewLink(resolved)
	now, err := filepath.EvalSymlinks(link)
	if err != nil || now == resolved {
		return part{name: "binary", state: stCurrent, detail: "Homebrew has nothing newer"}, ""
	}
	return part{name: "binary", state: stUpdated, detail: "by Homebrew (" + now + ")"}, link
}

// reexecFiles has the new binary renew the project files (crit. 8): the old
// process would install its own, older hooks and guide.
func reexecFiles(cmd *cobra.Command, newExe, projDir string) error {
	c := exec.Command(newExe, "update", "--files-only", "--dir", projDir)
	c.Stdout, c.Stderr = cmd.OutOrStdout(), cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		return fmt.Errorf("cerne update: the new binary (%s) did not finish renewing the project files (%v) — run: cerne update", newExe, err)
	}
	return nil
}
