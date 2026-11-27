package llm

import (
	"context"
	"slices"
	"strings"
	"testing"
)

var testImages = []string{"/cache/chat-attachments/c1/aa.png", "/cache/chat-attachments/c1/bb.jpg"}

// F-0520 crit. 5: the agentic claude turn opens the attachments' directory
// with --add-dir, once per directory.
func TestImagesClaudeAddsAttachmentDir(t *testing.T) {
	var got []string
	fake := func(_ context.Context, _, _ string, args []string, _ string) (string, string, error) {
		got = args
		return "ok", "", nil
	}
	p := &cliProvider{bin: "claude", run: fake, dir: "/proj", agentTools: AgentReadTools}
	if _, err := p.Complete(WithImages(context.Background(), testImages), "q"); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(" "+strings.Join(got, " ")+" ", " --add-dir /cache/chat-attachments/c1 "); n != 1 {
		t.Fatalf("argv %v has %d --add-dir for the attachment dir, want 1", got, n)
	}

	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "--add-dir") {
		t.Fatalf("argv %v has --add-dir with no image in the turn", got)
	}
}

// F-0520 crit. 6: codex gets one -i per image, and the stdin "-" stays last.
func TestImagesCodexAttachesEachFile(t *testing.T) {
	var args []string
	var stdin, dir string
	p := &cliProvider{kind: ModeCodexCLI, bin: "codex", run: codexFake("ok", &args, &stdin, &dir)}
	if _, err := p.Complete(WithImages(context.Background(), testImages), "q"); err != nil {
		t.Fatal(err)
	}
	j := " " + strings.Join(args, " ") + " "
	for _, img := range testImages {
		if !strings.Contains(j, " -i "+img+" ") {
			t.Fatalf("argv %v lacks -i %s", args, img)
		}
	}
	if args[len(args)-1] != "-" {
		t.Fatalf("argv %v must end with the stdin marker", args)
	}

	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(args, "-i") {
		t.Fatalf("argv %v has -i with no image in the turn", args)
	}
}

// F-0520 crit. 7: copilot gets one --attachment per image.
func TestImagesCopilotAttachesEachFile(t *testing.T) {
	var calls int
	var got copilotCall
	p := &cliProvider{kind: ModeCopilotCLI, bin: "copilot", run: copilotFake(copilotStream("ok", "m"), &calls, &got)}
	if _, err := p.Complete(WithImages(context.Background(), testImages), "q"); err != nil {
		t.Fatal(err)
	}
	j := " " + strings.Join(got.args, " ") + " "
	for _, img := range testImages {
		if !strings.Contains(j, " --attachment "+img+" ") {
			t.Fatalf("argv %v lacks --attachment %s", got.args, img)
		}
	}

	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got.args, "--attachment") {
		t.Fatalf("argv %v has --attachment with no image in the turn", got.args)
	}
}

// F-0524 crit. 1, 2, 3: cursor-agent gets one --image per image in order,
// keeps the exact text-only argv when there are none, and leaves cursorDenyAll
// denying every tool (Read included) in both cases.
func TestImagesCursorAttachesEachFile(t *testing.T) {
	var calls int
	var args []string
	var stdin, dir, cfg string
	p := &cliProvider{kind: ModeCursorCLI, bin: "cursor-agent", model: "gpt-5", run: cursorFake("ok", &calls, &args, &stdin, &dir, &cfg)}
	if _, err := p.Complete(WithImages(context.Background(), testImages), "q"); err != nil {
		t.Fatal(err)
	}
	wantWithImages := []string{
		"-p", "--output-format", "text", "--trust", "--mode", "ask", "--sandbox", "enabled",
		"--workspace", dir, "--model", "gpt-5",
		"--image", testImages[0], "--image", testImages[1],
	}
	if !slices.Equal(args, wantWithImages) {
		t.Fatalf("argv with images = %v, want %v", args, wantWithImages)
	}
	if cfg != cursorDenyAll || !strings.Contains(cfg, `"Read(**)"`) {
		t.Fatalf("workspace .cursor/cli.json with images = %q, want cursorDenyAll", cfg)
	}

	p.model = ""
	if _, err := p.Complete(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	wantPlain := []string{
		"-p", "--output-format", "text", "--trust", "--mode", "ask", "--sandbox", "enabled",
		"--workspace", dir,
	}
	if !slices.Equal(args, wantPlain) {
		t.Fatalf("argv without images = %v, want %v", args, wantPlain)
	}
	if cfg != cursorDenyAll {
		t.Fatalf("workspace .cursor/cli.json without images = %q, want cursorDenyAll", cfg)
	}
}

// F-0520 crit. 8; F-0524 crit. 5, 6: only the CLIs with an image input and
// byo-key take images; gemini-cli still does not.
func TestImagesSupportedModes(t *testing.T) {
	for mode, want := range map[string]bool{
		ModeClaudeCLI: true, ModeCodexCLI: true, ModeCopilotCLI: true, ModeCursorCLI: true,
		ModeGeminiCLI: false, ModeDelegation: false, ModeByoKey: true, // F-0522
	} {
		if got := SupportsImages(mode); got != want {
			t.Errorf("SupportsImages(%q) = %v, want %v", mode, got, want)
		}
	}
}
