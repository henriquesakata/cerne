//go:build !windows

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Black-box tests of the INSTALL.md "Shell completion" section (F-0509): each
// shell's block runs in a fresh home, then a new shell of that kind is asked
// to complete `cerne fea`. A shell missing on this machine is skipped; the
// result records a run where all four are present.

var completionShells = []string{"bash", "zsh", "fish", "powershell"}

// completionSection returns the INSTALL.md "## Shell completion" section.
func completionSection(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../INSTALL.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	start := strings.Index(doc, "\n## Shell completion\n")
	if start < 0 {
		t.Fatal("INSTALL.md has no '## Shell completion' section")
	}
	sec := doc[start+1:]
	if end := strings.Index(sec[3:], "\n## "); end >= 0 {
		sec = sec[:end+3]
	}
	return sec
}

var fenceRe = regexp.MustCompile("(?ms)^```([a-z]+)\n(.*?)^```")

// completionBlocks maps each fence language of the section to its code.
func completionBlocks(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range fenceRe.FindAllStringSubmatch(completionSection(t), -1) {
		out[m[1]] += m[2]
	}
	return out
}

// F-0509 crit. 1, 3
func TestInstallCompletion_Section(t *testing.T) {
	sec, blocks := completionSection(t), completionBlocks(t)
	for _, sh := range completionShells {
		code, ok := blocks[sh]
		if !ok {
			t.Errorf("no ```%s block in the section", sh)
		}
		for _, bad := range []string{"sudo", "/etc/", "/usr/"} {
			if strings.Contains(code, bad) {
				t.Errorf("%s block writes outside the user's home (%q):\n%s", sh, bad, code)
			}
		}
	}
	for _, want := range []string{"bash-completion", "brew install bash-completion@2", "compinit", "new shell"} {
		if !strings.Contains(sec, want) {
			t.Errorf("section does not mention %q", want)
		}
	}
}

// F-0509 crit. 2
func TestInstallCompletion_Shells(t *testing.T) {
	blocks := completionBlocks(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, sh := range completionShells {
		t.Run(sh, func(t *testing.T) {
			exe := map[string]string{"powershell": "pwsh"}[sh]
			if exe == "" {
				exe = sh
			}
			if _, err := exec.LookPath(exe); err != nil {
				t.Skipf("%s not installed", exe)
			}
			if sh == "bash" && bashCompletion() == "" {
				t.Skip("bash-completion 2.x not installed")
			}
			home := t.TempDir()
			bin := filepath.Join(home, "test-bin")
			if err := os.MkdirAll(bin, 0o755); err != nil {
				t.Fatal(err)
			}
			// The cerne on PATH is this test binary in CLI mode (main_test.go).
			shim := "#!/bin/sh\nCERNE_TEST_RUN_CLI=1 exec '" + self + "' \"$@\"\n"
			if err := os.WriteFile(filepath.Join(bin, "cerne"), []byte(shim), 0o755); err != nil {
				t.Fatal(err)
			}
			env := []string{"HOME=" + home, "PATH=" + bin + ":" + os.Getenv("PATH"), "TERM=dumb"}
			for _, kv := range os.Environ() {
				k := kv[:strings.Index(kv, "=")]
				if k != "HOME" && k != "PATH" && k != "TERM" && !strings.HasPrefix(k, "XDG_") && k != "BASH_COMPLETION_USER_DIR" && k != "ZDOTDIR" {
					env = append(env, kv)
				}
			}
			run := func(args ...string) string {
				t.Helper()
				c := exec.Command(exe, args...)
				c.Dir, c.Env = home, env
				var stderr strings.Builder
				c.Stderr = &stderr // cobra's __complete logs its directive there
				out, err := c.Output()
				if err != nil {
					t.Fatalf("%s %v: %v\n%s%s", exe, args, err, out, stderr.String())
				}
				return string(out)
			}

			var got string
			want := regexp.MustCompile(`(?m)^feature\b`)
			switch sh {
			case "bash":
				run("-c", blocks[sh])
				got = run("-c", `source `+bashCompletion()+`
if declare -F _comp_load >/dev/null; then _comp_load cerne; else __load_completion cerne; fi
COMP_WORDS=(cerne fea); COMP_CWORD=1; COMP_LINE='cerne fea'; COMP_POINT=9
__start_cerne cerne fea cerne
printf '%s\n' "${COMPREPLY[@]}"`)
			case "zsh":
				// A user with completion already on, as the section assumes.
				if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("autoload -U compinit; compinit -u\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				run("-c", blocks[sh])
				// zsh has no completion API outside the line editor: the
				// completer being registered for cerne is what is checked.
				got = run("-i", "-c", `print -r -- "${_comps[cerne]}"`)
				want = regexp.MustCompile(`(?m)^_cerne$`)
			case "fish":
				run("-c", blocks[sh])
				got = run("-c", "complete -C 'cerne fea'")
			case "powershell":
				run("-NoLogo", "-NonInteractive", "-Command", blocks[sh])
				got = run("-NoLogo", "-NonInteractive", "-Command",
					"(TabExpansion2 -inputScript 'cerne fea' -cursorColumn 9).CompletionMatches.CompletionText")
			}
			if !want.MatchString(got) {
				t.Errorf("%s did not complete 'cerne fea' (want %s):\n%s", sh, want, got)
			}
		})
	}
}

// bashCompletion is the bash-completion 2.x entry script, or "".
func bashCompletion() string {
	for _, p := range []string{"/usr/share/bash-completion/bash_completion", "/opt/homebrew/share/bash-completion/bash_completion", "/usr/local/share/bash-completion/bash_completion"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
