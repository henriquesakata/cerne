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

// F-0520 crit. 8: only the CLIs with an image input take images.
func TestImagesSupportedModes(t *testing.T) {
	for mode, want := range map[string]bool{
		ModeClaudeCLI: true, ModeCodexCLI: true, ModeCopilotCLI: true,
		ModeGeminiCLI: false, ModeCursorCLI: false, ModeDelegation: false, "byo-key": false,
	} {
		if got := SupportsImages(mode); got != want {
			t.Errorf("SupportsImages(%q) = %v, want %v", mode, got, want)
		}
	}
}
