package project

import (
	"errors"
	"testing"
)

func fakeLook(have ...string) func(string) (string, error) {
	return func(n string) (string, error) {
		for _, h := range have {
			if h == n {
				return "/bin/" + n, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestDetectCLIOrderAndNone(t *testing.T) {
	t.Setenv("CERNE_NO_CLI_DETECT", "")
	cases := []struct {
		have []string
		want string
	}{
		{[]string{"gemini", "codex", "claude"}, "claude"},
		{[]string{"gemini", "codex"}, "codex"},
		{[]string{"gemini"}, "gemini"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := firstCLI(fakeLook(c.have...)); got != c.want {
			t.Errorf("have %v: got %q want %q", c.have, got, c.want)
		}
	}
}

func TestDetectCLIDisabledSkipsPATH(t *testing.T) {
	t.Setenv("CERNE_NO_CLI_DETECT", "1")
	look := func(string) (string, error) { t.Fatal("PATH consulted"); return "", nil }
	if got := firstCLI(look); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestTestMainDisablesDetection(t *testing.T) {
	if DetectSubscriptionCLI() != "" {
		t.Fatal("detection must be off under go test")
	}
}

func firstCLI(look func(string) (string, error)) string {
	if f := foundCLIs(look); len(f) > 0 {
		return f[0]
	}
	return ""
}

func TestFoundCLIsListsAllInOrder(t *testing.T) {
	t.Setenv("CERNE_NO_CLI_DETECT", "")
	got := foundCLIs(fakeLook("copilot", "cursor-agent", "gemini", "claude"))
	if len(got) != 4 || got[0] != "claude" || got[1] != "gemini" || got[2] != "cursor-agent" || got[3] != "copilot" {
		t.Fatalf("got %v", got)
	}
}

// F-0441: the Cursor binary is cursor-agent but its mode is cursor-cli, so the
// mode is no longer the binary name plus "-cli".
func TestCLIMode(t *testing.T) {
	for bin, want := range map[string]string{"claude": "claude-cli", "codex": "codex-cli", "gemini": "gemini-cli", "cursor-agent": "cursor-cli", "copilot": "copilot-cli"} {
		if got := CLIMode(bin); got != want {
			t.Errorf("CLIMode(%q) = %q, want %q", bin, got, want)
		}
	}
}

// F-0414 (D1): detection is per call — an install made while `cerne ui` runs
// shows up on the next request.
func TestFoundSubscriptionCLIsIsNotCached(t *testing.T) {
	t.Setenv("CERNE_NO_CLI_DETECT", "")
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = fakeLook("claude")
	if got := FoundSubscriptionCLIs(); len(got) != 1 {
		t.Fatalf("first: %v", got)
	}
	lookPath = fakeLook("claude", "codex")
	if got := FoundSubscriptionCLIs(); len(got) != 2 {
		t.Fatalf("second call must re-detect: %v", got)
	}
}
