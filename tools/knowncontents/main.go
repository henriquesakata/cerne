// Command knowncontents maintains internal/refresh/known_contents.json, the
// hash of every managed item some release wrote (F-0477). Run it from the
// repository root.
//
//	go run ./tools/knowncontents          appends what THIS build writes (run it
//	                                      whenever a managed file changes, and commit)
//	go run ./tools/knowncontents tags     the one-time backfill: for every release
//	                                      tag in this clone, builds that release,
//	                                      installs the agents in a throwaway project
//	                                      and records the hashes; a tag already in
//	                                      the file is not redone, one that cannot be
//	                                      built is recorded as skipped with the reason
//	go run ./tools/knowncontents fixture <tag> <dest>   a release's install, as test data
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/henriquesakata/cerne/internal/refresh"
)

// errNoHooks marks a release from before the hooks existed (v0.4.0): nothing to hash.
var errNoHooks = errors.New("this release has no hook command")

const listPath = "internal/refresh/known_contents.json"

func main() {
	if len(os.Args) == 4 && os.Args[1] == "fixture" { // fixture <tag> <dest>: the tag's install, kept as test data
		if err := fixture(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, "knowncontents:", err)
			os.Exit(1)
		}
		return
	}
	var err error
	switch {
	case len(os.Args) == 1:
		err = update(appendCurrent)
	case len(os.Args) == 2 && os.Args[1] == "tags":
		err = update(appendTags)
	default:
		err = errors.New("usage: knowncontents [tags | fixture <tag> <dest>]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "knowncontents:", err)
		os.Exit(1)
	}
}

// appendCurrent adds what this build installs.
func appendCurrent(list *refresh.Contents) error {
	items := refresh.Current()
	if len(items) == 0 {
		return errors.New("this build's install read back no managed item")
	}
	add(list, items)
	fmt.Fprintf(os.Stderr, "current build: %d items\n", len(items))
	return nil
}

func add(list *refresh.Contents, items map[string]string) {
	for slot, sha := range items {
		if !contains(list.Known[slot], sha) {
			list.Known[slot] = append(list.Known[slot], sha)
		}
	}
}

func update(fill func(*refresh.Contents) error) error {
	var list refresh.Contents
	if raw, err := os.ReadFile(listPath); err == nil {
		if err := json.Unmarshal(raw, &list); err != nil {
			return err
		}
	}
	if list.Tags == nil {
		list.Tags = map[string]string{}
	}
	if list.Known == nil {
		list.Known = map[string][]string{}
	}
	if err := fill(&list); err != nil {
		return err
	}
	for _, shas := range list.Known {
		sort.Strings(shas)
	}
	raw, err := json.MarshalIndent(list, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(listPath, append(raw, '\n'), 0o644)
}

// appendTags is the one-time backfill over the release tags of this clone.
func appendTags(list *refresh.Contents) error {
	out, err := exec.Command("git", "tag", "-l", "v*").Output()
	if err != nil {
		return err
	}
	for _, tag := range strings.Fields(string(out)) {
		if _, done := list.Tags[tag]; done {
			continue
		}
		items, err := itemsOf(tag)
		if err != nil {
			list.Tags[tag] = err.Error()
			fmt.Fprintf(os.Stderr, "%s: skipped: %v\n", tag, err)
			continue
		}
		list.Tags[tag] = ""
		add(list, items)
		fmt.Fprintf(os.Stderr, "%s: %d items\n", tag, len(items))
	}
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// itemsOf builds tag's binary and reads back what it installs.
func itemsOf(tag string) (map[string]string, error) {
	tmp, err := os.MkdirTemp("", "knowncontents-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	proj, err := installTag(tag, tmp)
	if errors.Is(err, errNoHooks) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := refresh.Collect(proj)
	if len(items) == 0 {
		return nil, fmt.Errorf("the install wrote no managed item")
	}
	return items, nil
}

// fixture leaves in dest the managed files tag installs: test data that comes
// from the release itself, not from a hand copy.
func fixture(tag, dest string) error {
	tmp, err := os.MkdirTemp("", "knowncontents-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	proj, err := installTag(tag, tmp)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(proj, func(p string, e fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(proj, p)
		switch {
		case err != nil:
			return err
		case rel == ".git" || rel == ".cernerc":
			if e.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case e.IsDir():
			return os.MkdirAll(filepath.Join(dest, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, rel), b, 0o644)
	})
}

// archive extracts tag's tree into dir with git's tar piped to tar, through
// pipes and argument lists, never a shell.
func archive(tag, dir string) (string, error) {
	gitCmd := exec.Command("git", "archive", "--format=tar", tag)
	tarCmd := exec.Command("tar", "-x", "-C", dir)
	pipe, err := gitCmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	tarCmd.Stdin = pipe
	var gitErr, tarErr bytes.Buffer
	gitCmd.Stderr, tarCmd.Stderr = &gitErr, &tarErr
	if err := tarCmd.Start(); err != nil {
		return "", err
	}
	if err := gitCmd.Run(); err != nil {
		_ = tarCmd.Process.Kill()
		_ = tarCmd.Wait()
		return gitErr.String(), err
	}
	if err := tarCmd.Wait(); err != nil {
		return tarErr.String(), err
	}
	return "", nil
}

// installTag builds tag's binary in tmp and installs every agent it knows into
// a throwaway project, returned.
func installTag(tag, tmp string) (string, error) {
	src, proj, home := filepath.Join(tmp, "src"), filepath.Join(tmp, "proj"), filepath.Join(tmp, "home")
	for _, d := range []string{src, proj, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	// git archive: the tag's tree without touching this worktree.
	if out, err := archive(tag, src); err != nil {
		return "", fmt.Errorf("cannot extract %s: %s", tag, out)
	}
	bin := filepath.Join(tmp, "cerne")
	if out, err := sh(src, nil, "go", "build", "-o", bin, "./cmd/cerne"); err != nil {
		return "", fmt.Errorf("does not build: %s", tail(out))
	}
	env := []string{"HOME=" + home, "CERNE_CACHE_DIR=" + filepath.Join(tmp, "cache"), "CERNE_CONFIG_DIR=" + filepath.Join(tmp, "config"), "XDG_CONFIG_HOME=" + filepath.Join(tmp, "xdg")}
	for _, d := range []string{".cursor", ".codex", ".gemini", ".github/skills"} {
		if err := os.MkdirAll(filepath.Join(proj, d), 0o755); err != nil {
			return "", err
		}
	}
	if out, err := sh(proj, env, "git", "init", "-q"); err != nil {
		return "", fmt.Errorf("git init: %s", out)
	}
	// A project root the old installers accept: none of them reads the backend.
	if err := os.WriteFile(filepath.Join(proj, ".cernerc"), []byte("project_id = \"probe\"\nbackend = \""+filepath.Join(tmp, "backend.git")+"\"\n"), 0o644); err != nil {
		return "", err
	}
	if out, err := sh(proj, env, bin, "hook", "install", "--dir", proj); err != nil {
		if strings.Contains(out, `unknown command "hook"`) {
			return "", errNoHooks
		}
		return "", fmt.Errorf("hook install fails: %s", tail(out))
	}
	// An older release does not know every agent: its refusal means it wrote nothing for it.
	for _, p := range []string{"cursor", "codex", "gemini", "copilot"} {
		_, _ = sh(proj, env, bin, "hook", "install", "--dir", proj, "--provider", p)
	}
	_, _ = sh(proj, env, bin, "skill", "install", "--dir", proj) // no guide before the skill existed
	return proj, nil
}

func sh(dir string, env []string, name string, args ...string) (string, error) {
	c := exec.Command(name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	out, err := c.CombinedOutput()
	return string(out), err
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}
