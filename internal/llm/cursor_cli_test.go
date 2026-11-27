package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// cursorFake records the call and the permissions file the provider wrote into
// the workspace, read while the call runs (the directory is gone afterwards).
func cursorFake(reply string, calls *int, gotArgs *[]string, gotStdin, gotDir, gotCfg *string) runner {
	return func(_ context.Context, dir, _ string, args []string, stdin string) (string, string, error) {
		*calls++
		*gotArgs, *gotStdin, *gotDir = args, stdin, dir
		b, _ := os.ReadFile(filepath.Join(dir, ".cursor", "cli.json"))
		*gotCfg = string(b)
		return reply, "", nil
	}
}

// F-0441 criteria 1, 2 and 6: one-shot text through cursor-agent -p, prompt on
// stdin, in an empty workspace whose permissions deny every tool, in ask mode
// with the sandbox on; --model only when a model is set.
func TestCursorCLIProvider(t *testing.T) {
	var calls int
	var args []string
	var stdin, dir, cfg string
	p := &cliProvider{kind: ModeCursorCLI, bin: "cursor-agent", model: "gpt-5", run: cursorFake("  the answer\n", &calls, &args, &stdin, &dir, &cfg)}
	out, err := p.Complete(context.Background(), "question?")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out != "the answer" || stdin != "question?" {
		t.Fatalf("out = %q, stdin = %q", out, stdin)
	}
	want := []string{"-p", "--output-format", "text", "--trust", "--mode", "ask", "--sandbox", "enabled",
		"--workspace", dir, "--model", "gpt-5"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	var perms struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(cfg), &perms); err != nil {
		t.Fatalf("workspace .cursor/cli.json = %q: %v", cfg, err)
	}
	if d := []string{"Shell(*)", "Read(**)", "Write(**)", "WebFetch(*)", "Mcp(*:*)"}; !reflect.DeepEqual(perms.Permissions.Deny, d) || len(perms.Permissions.Allow) != 0 {
		t.Fatalf("permissions = %+v, want deny %v and no allow", perms.Permissions, d)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("workspace must be removed after the call")
	}
	p.model = ""
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if a == "--model" {
			t.Fatalf("--model passed without a model: %v", args)
		}
	}
}

// Criterion 4: a missing CLI or login names the fix, never Claude's.
func TestCursorCLIErrorsNameTheFix(t *testing.T) {
	if got := cliProduct("cursor-agent"); got != "Cursor CLI" {
		t.Fatalf("cliProduct = %q", got)
	}
	for _, c := range []struct{ err, want string }{
		{`could not run "cursor-agent" (install Cursor CLI and ensure it is on PATH): x`, "cursor.com/install"},
		{`"cursor-agent" exited with exit status 1: Authentication required`, "cursor-agent login"},
	} {
		p := &cliProvider{kind: ModeCursorCLI, bin: "cursor-agent", run: func(context.Context, string, string, []string, string) (string, string, error) {
			return "", "", errors.New(c.err)
		}}
		_, err := p.Complete(context.Background(), "q")
		if !errors.Is(err, ErrCursorCLI) || errors.Is(err, ErrClaudeCLI) {
			t.Fatalf("err = %v, want ErrCursorCLI only", err)
		}
		if !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), "Claude") {
			t.Fatalf("err = %v, want %q and no Claude wording", err, c.want)
		}
	}
}

// Criterion 5: an isolation flag the CLI does not know is never dropped.
func TestCursorCLIRejectedFlag(t *testing.T) {
	calls := 0
	p := &cliProvider{kind: ModeCursorCLI, bin: "cursor-agent", run: func(context.Context, string, string, []string, string) (string, string, error) {
		calls++
		return "", "", errors.New(`"cursor-agent" exited with exit status 1: error: unknown option '--sandbox'`)
	}}
	_, err := p.Complete(context.Background(), "q")
	if !errors.Is(err, ErrCursorCLI) || !strings.Contains(err.Error(), "cursor-agent update") || calls != 1 {
		t.Fatalf("err = %v after %d calls, want the update instruction and no retry", err, calls)
	}
}

func TestCursorCLIEmptyAnswerIsAnError(t *testing.T) {
	var calls int
	var a []string
	var s, d, c string
	p := &cliProvider{kind: ModeCursorCLI, bin: "cursor-agent", run: cursorFake("  \n", &calls, &a, &s, &d, &c)}
	if _, err := p.Complete(context.Background(), "q"); !errors.Is(err, ErrCursorCLI) {
		t.Fatalf("err = %v, want ErrCursorCLI", err)
	}
}

// F-0524 crit. 4: if cursor-agent rejects --image or fails to open an attached
// image, Complete returns ErrCursorCLI after one call and never retries
// without the images.
func TestImagesCursorFailureDoesNotRetryWithoutImages(t *testing.T) {
	for name, tc := range map[string]struct {
		cliErr string
		want   string
	}{
		"unknown --image flag": {
			cliErr: `"cursor-agent" exited with exit status 1: error: unknown option '--image'`,
			want:   "does not know --image — update it (cursor-agent update)",
		},
		"unreadable attached image": {
			cliErr: `"cursor-agent" exited with exit status 1: Error: ENOENT: no such file or directory, open '/cache/chat-attachments/c1/aa.png'`,
			want:   "aa.png",
		},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			var seenArgs [][]string
			p := &cliProvider{kind: ModeCursorCLI, bin: "cursor-agent", run: func(_ context.Context, _ string, _ string, args []string, _ string) (string, string, error) {
				calls++
				seenArgs = append(seenArgs, append([]string(nil), args...))
				return "", "", errors.New(tc.cliErr)
			}}
			_, err := p.Complete(WithImages(context.Background(), testImages), "describe")
			if !errors.Is(err, ErrCursorCLI) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrCursorCLI containing %q", err, tc.want)
			}
			if calls != 1 {
				t.Fatalf("calls = %d (%v), want 1 with no retry without images", calls, seenArgs)
			}
		})
	}
}
