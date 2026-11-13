package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/henriquesakata/cerne/internal/doclang"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/hooks"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/skills"
	"github.com/henriquesakata/cerne/internal/slug"
	"github.com/henriquesakata/cerne/internal/store"
)

// ErrCacheBackendMismatch: the project id's cache already holds a clone of a
// different backend — another project with the same name, or a leftover from
// a failed init. init refuses rather than deleting it (it may be a live
// project's clone with unpublished state) (F-0335).
// ErrDocsLanguageMismatch reports an init that joins an existing backend
// while asking for a documentation language other than the backend's.
var ErrDocsLanguageMismatch = errors.New("documentation language differs from the backend's")

var ErrCacheBackendMismatch = errors.New("cached backend clone points at a different backend")

func newInitCmd() *cobra.Command {
	var (
		name    string
		backend string
		modules []string
		dir     string
		docs    string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a cerne project in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			targetDir := dir
			if targetDir == "" {
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				targetDir = wd
			}
			return runInit(cmd, targetDir, name, backend, modules, docs)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "project name")
	cmd.Flags().StringVar(&backend, "backend", "", "backend git URL or local path (default: a local bare repo in the per-user data dir)")
	cmd.Flags().StringArrayVar(&modules, "module", nil, "module to register (repeatable)")
	cmd.Flags().StringVar(&docs, "docs-language", "", "language cerne scaffolds documents in, for a new backend: en (default) or pt-BR")
	cmd.Flags().StringVar(&dir, "dir", "", "target project directory (defaults to cwd)")
	_ = cmd.Flags().MarkHidden("dir")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

func runInit(cmd *cobra.Command, targetDir, name, backend string, modules []string, docs string) (err error) {
	// Validated before anything is cloned or written.
	docsLang := doclang.EN
	if docs != "" {
		if docsLang, err = doclang.Parse(docs); err != nil {
			return err
		}
	}
	projectID := slug.Make(name)
	if projectID == "" {
		return fmt.Errorf("project name %q yields an empty project id", name)
	}
	// No --backend: the backend is a local bare repo at a stable per-project
	// path outside the cache; it is created below unless it already exists, in
	// which case init joins it like any existing backend (F-0365).
	localBackend := backend == ""
	if localBackend {
		if backend, err = project.BackendDataDir(projectID); err != nil {
			return err
		}
	}
	if gitback.IsLocalPath(backend) {
		// Absolute before anything records it: the URL is stored verbatim as
		// the clone's origin, and later git runs inside the cache, where a
		// relative path points elsewhere (F-0335).
		if backend, err = filepath.Abs(backend); err != nil {
			return err
		}
	}
	if _, foundDir, err := project.LoadConfig(targetDir); err == nil {
		return fmt.Errorf("project already initialized: .cernerc found in %s", foundDir)
	} else if !errors.Is(err, project.ErrNoProject) {
		return err
	}

	if localBackend {
		if _, err := os.Stat(filepath.Join(backend, "HEAD")); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(backend), 0o755); err != nil {
				return err
			}
			if err := gitback.InitBare(backend); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}

	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return err
	}
	repoDir := filepath.Join(cacheDir, "repo")
	// created marks a clone this init made: until the project is configured
	// it owns it, and a failure removes it so the retry clones fresh instead
	// of refreshing a dead origin or "joining" an unpublished layout (F-0335).
	created := false
	defer func() {
		if created && err != nil {
			_ = os.RemoveAll(repoDir)
		}
	}()
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		// The cache is keyed by project id alone: a clone of another backend
		// is another project with the same name (or a pre-F-0335 leftover).
		cached, err := gitback.OriginURL(repoDir)
		if err != nil {
			return err
		}
		if cached != backend {
			return fmt.Errorf("%w: %s tracks %s, not %s (another project named %q, or a leftover from a failed init): delete that directory or pick a different --name",
				ErrCacheBackendMismatch, repoDir, cached, backend, projectID)
		}
		// Cache already holds a clone of this project's backend: refresh it.
		// SafePull, not Pull — the clone is shared with concurrent cerne
		// processes (a ui pull loop, a synthesis in flight), and a raw Pull on
		// a dirty worktree poisons it (N-0027: ref moved, files left behind).
		// Now, not the eliding variant: init is a one-shot setting the project
		// up, so a skipped fetch (F-0200) saves nothing and risks initializing
		// against a stale snapshot.
		if err := gitback.SafePullNow(repoDir); err != nil {
			return fmt.Errorf("refreshing cached backend clone at %s failed (delete that directory to force a fresh clone): %w", repoDir, err)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return err
		}
		created = true
		if err := gitback.Clone(backend, repoDir); err != nil {
			return err
		}
	} else {
		return err
	}

	joined := true
	if _, err := store.LoadProject(repoDir); errors.Is(err, store.ErrNotFound) {
		joined = false
		// A new backend is born English unless told otherwise (F-0359).
		if err := store.InitLayout(repoDir, store.Project{Name: name, Modules: modules, DocsLanguage: string(docsLang)}); err != nil {
			return err
		}
		if _, err := gitback.CommitAndPush(repoDir, "cerne: initialize backend layout"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if have := store.DocsLanguage(repoDir); docs != "" && have != docsLang {
		// The language belongs to the backend: a second checkout joins it, it
		// does not pick its own.
		return fmt.Errorf("%w: this backend's documents are in %s; drop --docs-language to join it", ErrDocsLanguageMismatch, have)
	}

	head, err := gitback.HeadSHA(repoDir)
	if err != nil {
		return err
	}
	// A new project starts in light mode (F-0364): the bar and the Gauntlet
	// record are an opt-in, not the first hour's wall. The invitation below
	// says how to turn it on. Joining an existing backend picks no mode: that
	// project may run with the Gauntlet, and its own .cernerc says so.
	cfg := project.Config{ProjectID: projectID, Backend: backend}
	if !joined {
		cfg.Gates.Gauntlet = "off"
	}
	if err := project.SaveConfig(targetDir, cfg); err != nil {
		return err
	}
	created = false // the configured project owns the clone now
	lock := project.Lock{Version: 1, Backend: backend, Snapshot: head}
	if err := project.SaveLock(targetDir, lock); err != nil {
		return err
	}
	docsDir := filepath.Join(targetDir, ".docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(docsDir, ".gitkeep"), nil, 0o644); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if joined {
		fmt.Fprintf(out, "Joined existing backend %s as project %q.\n", backend, projectID)
	} else {
		fmt.Fprintf(out, "Initialized project %q with backend %s.\n", projectID, backend)
	}

	if localBackend {
		fmt.Fprintf(out, "Backend is local: %s. To move it to a remote: git -C %q push --mirror <url>, set backend = \"<url>\" in .cernerc, then delete %q (the cached clone still points here).\n", backend, backend, repoDir)
	}

	// Install cerne's canonical Claude Code skills so a new project ships with
	// the agent guide. The project is already provisioned at this point, so a
	// skills failure must not fail init — warn and continue.
	det := hooks.DetectProviders(targetDir)
	if results, err := skills.Install(targetDir, det...); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not install cerne's skills: %v\n", err)
	} else {
		printSkillResults(out, results)
		printAgentLines(out, results, det)
	}

	// Install cerne's Claude Code hooks too (F-0139): the provenance gate and
	// the SessionStart registration reminder, so a new project is born with the
	// register-before-implementing rules injected. Same non-fatal stance.
	if results, err := hooks.Install(targetDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not install cerne's hooks: %v\n", err)
	} else {
		printHookResults(out, results)
	}

	// Wire docs.lock to the merge driver so concurrent branches don't conflict
	// on the snapshot pin. Non-fatal: the project is already provisioned.
	if err := ensureLockMergeDriver(targetDir); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not register the docs.lock merge driver: %v\n", err)
	}

	// Keep the generated projection out of version control (F-0370). Same
	// non-fatal stance as the merge driver above.
	docsIgnored, err := ensureDocsIgnored(targetDir)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
	} else if docsIgnored {
		fmt.Fprintln(out, "Added .docs/ to .gitignore (the generated projection; never commit it).")
	}

	if !joined {
		fmt.Fprintln(out, `Gauntlet is off (light mode). Turn it on in the Settings screen of cerne ui, or run: cerne settings set gates.gauntlet on --scope project`)
	}
	fmt.Fprintln(out, "Next steps:")
	toCommit := ".cernerc, docs.lock and .gitattributes"
	if docsIgnored {
		toCommit = ".cernerc, docs.lock, .gitattributes and .gitignore"
	}
	fmt.Fprintf(out, "  - commit %s to your project repository\n", toCommit)
	fmt.Fprintln(out, "Open the dashboard: cerne ui  (http://localhost:7717)")
	return nil
}

// ErrGitignoreSymlink: .gitignore is a symlink, so init will not write through it.
var ErrGitignoreSymlink = errors.New(`.gitignore is a symlink; add ".docs/" to it yourself`)

// ensureDocsIgnored appends ".docs/" to dir's .gitignore unless git already
// ignores it by any rule (F-0370). Append-only, mirrors the file's line ending,
// never creates a .gitignore outside a git work tree and never writes through a
// symlink. init requires the git binary, so there is no git-less fallback.
func ensureDocsIgnored(dir string) (changed bool, err error) {
	git := func(args ...string) (string, string, error) {
		var o, e strings.Builder
		c := exec.Command("git", args...)
		c.Dir, c.Stdout, c.Stderr = dir, &o, &e
		c.Env = append(os.Environ(), "LC_ALL=C")
		err := c.Run()
		return strings.TrimSpace(o.String()), e.String(), err
	}
	out, stderr, err := git("rev-parse", "--is-inside-work-tree")
	if err != nil {
		if strings.Contains(stderr, "not a git repository") {
			return false, nil
		}
		return false, fmt.Errorf("could not check .gitignore: %w: %s", err, strings.TrimSpace(stderr))
	}
	if out != "true" {
		return false, nil // bare repository or inside .git: no work tree
	}
	switch _, stderr, err := git("check-ignore", "-q", ".docs/"); {
	case err == nil:
		return false, nil // already ignored
	case isExit(err, 1):
		// not ignored: append below
	default:
		return false, fmt.Errorf("could not check .gitignore: %w: %s", err, strings.TrimSpace(stderr))
	}

	path := filepath.Join(dir, ".gitignore")
	if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return false, ErrGitignoreSymlink
	}
	eol, prefix := "\n", ""
	if b, rerr := os.ReadFile(path); rerr == nil {
		if strings.Contains(string(b), "\r\n") {
			eol = "\r\n"
		}
		if len(b) > 0 && b[len(b)-1] != '\n' {
			prefix = eol
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return false, fmt.Errorf("could not update .gitignore: %w", err)
	}
	if _, err := f.WriteString(prefix + ".docs/" + eol); err != nil {
		f.Close()
		return false, fmt.Errorf("could not update .gitignore: %w", err)
	}
	return true, f.Close()
}

func isExit(err error, code int) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == code
}
