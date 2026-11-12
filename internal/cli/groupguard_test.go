package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestGroupUnknownSubcommandFails(t *testing.T) { // F-0344 criterion 1
	_, err := execCerne(t, "work", "bogus")
	if err == nil || !strings.Contains(err.Error(), `unknown command "bogus" for "cerne work"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupSuggestsRootCommand(t *testing.T) { // criterion 2
	_, err := execCerne(t, "feature", "show", "F-1")
	if err == nil || !strings.Contains(err.Error(), "Did you mean: cerne show F-1?") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupSuggestsNearSubcommand(t *testing.T) { // criterion 3
	_, err := execCerne(t, "feature", "aprove")
	if err == nil || !strings.Contains(err.Error(), "approve") {
		t.Fatalf("err = %v", err)
	}
}

func TestGroupWithoutArgsPrintsHelp(t *testing.T) { // criterion 4
	for _, args := range [][]string{{"feature"}, {"feature", "--help"}} {
		out, err := execCerne(t, args...)
		if err != nil || !strings.Contains(out, "Available Commands") {
			t.Fatalf("%v: err=%v out=%q", args, err, out)
		}
	}
}

func TestGroupHelpArgSuggestsRootHelpWithPath(t *testing.T) { // review: `feature help approve`
	_, err := execCerne(t, "feature", "help", "approve")
	if err == nil || !strings.Contains(err.Error(), "Did you mean: cerne help feature approve?") {
		t.Fatalf("err = %v", err)
	}
	out, err := execCerne(t, "help", "feature", "approve")
	if err != nil || !strings.Contains(out, "Approve a candidate") {
		t.Fatalf("suggested command: err=%v out=%q", err, out)
	}
}

func TestCompletionGroupRejectsUnknownSubcommand(t *testing.T) { // review A: cobra adds it only at Execute
	_, err := execCerne(t, "completion", "bogus")
	if err == nil || !strings.Contains(err.Error(), `unknown command "bogus" for "cerne completion"`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := execCerne(t, "completion", "zsh"); err != nil { // script goes to os.Stdout
		t.Fatalf("completion zsh: %v", err)
	}
}

func TestGroupSuggestionIsNotCircular(t *testing.T) { // review B
	_, err := execCerne(t, "feature", "feature", "F-1")
	if err == nil || strings.Contains(err.Error(), "Did you mean: cerne feature F-1") {
		t.Fatalf("err = %v", err)
	}
}

func TestEveryGroupRejectsUnknownSubcommand(t *testing.T) { // criterion 5
	n := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.HasSubCommands() && sub.Run == nil && (sub.RunE == nil || reflect.ValueOf(sub.RunE).Pointer() == reflect.ValueOf(groupRunE).Pointer()) {
				// groups that kept their own RunE (e.g. accept) are out of scope
				t.Logf("group: %s", sub.CommandPath())
				n++
				args := strings.Fields(strings.TrimPrefix(sub.CommandPath(), "cerne "))
				_, err := execCerne(t, append(args, "zzbogus")...)
				if err == nil || !strings.Contains(err.Error(), `unknown command "zzbogus" for "`+sub.CommandPath()+`"`) {
					t.Errorf("%s: err = %v", sub.CommandPath(), err)
				}
			}
			walk(sub)
		}
	}
	walk(Root())
	if n == 0 {
		t.Fatal("no groups found")
	}
}
