package forge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F-0355 A3: the credential helper is run with every prompt channel closed and
// exactly the protocol/host on stdin. A fake git on PATH records both.
func TestFillCredentialClosesPromptsAndSendsOnlyHost(t *testing.T) {
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec")
	script := "#!/bin/sh\n" +
		"cat > " + rec + ".stdin\n" +
		"env > " + rec + ".env\n" +
		"printf 'username=root\\npassword=tok\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Values a host environment might carry must be overridden, not inherited.
	t.Setenv("GIT_ASKPASS", "/bin/evil-askpass")
	t.Setenv("GCM_INTERACTIVE", "always")

	user, secret := fillCredential(context.Background(), "git.example")
	if user != "root" || secret != "tok" {
		t.Fatalf("got %q %q", user, secret)
	}
	stdin, _ := os.ReadFile(rec + ".stdin")
	if string(stdin) != "protocol=https\nhost=git.example\n\n" {
		t.Fatalf("stdin = %q", stdin)
	}
	env, _ := os.ReadFile(rec + ".env")
	lines := strings.Split(string(env), "\n")
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=", "GCM_INTERACTIVE=never"} {
		found := false
		for _, l := range lines {
			if l == want {
				found = true
			}
		}
		if !found {
			t.Errorf("env lacks %q", want)
		}
	}
}
