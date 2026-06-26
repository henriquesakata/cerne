package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestErrataCreatesRecordOneShot(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	out, err := execCerne(t, "feature", "errata", "F-0001", "Correção: usar D-0002.", "--dir", projDir)
	if err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	if !strings.Contains(out, "errata anexada a F-0001") {
		t.Fatalf("missing confirmation:\n%s", out)
	}
	data, err := os.ReadFile(filepath.Join(repoDir, "content", "errata", "F-0001.md"))
	if err != nil || !strings.Contains(string(data), "usar D-0002") {
		t.Fatalf("errata file missing/wrong (err %v):\n%s", err, data)
	}
}

func TestErrataFromFile(t *testing.T) {
	projDir, _, repoDir := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	bodyFile := filepath.Join(t.TempDir(), "errata.txt")
	if err := os.WriteFile(bodyFile, []byte("Correção via arquivo."), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "--file", bodyFile, "--dir", projDir); err != nil {
		t.Fatalf("errata --file: %v\n%s", err, out)
	}
	data, _ := os.ReadFile(filepath.Join(repoDir, "content", "errata", "F-0001.md"))
	if !strings.Contains(string(data), "via arquivo") {
		t.Fatalf("errata --file body missing:\n%s", data)
	}
}

func TestErrataWiredOnEachEntity(t *testing.T) {
	root := Root()
	for _, parent := range []string{"feature", "decision", "runbook", "note"} {
		if !hasSubcommand(root, parent, "errata") {
			t.Errorf("%s errata subcommand not wired", parent)
		}
	}
}

func TestShowAppendsErrata(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "primeira nota", "--dir", projDir); err != nil {
		t.Fatalf("errata 1: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "segunda nota", "--dir", projDir); err != nil {
		t.Fatalf("errata 2: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "F-0001", "--dir", projDir)
	if err != nil {
		t.Fatalf("show: %v\n%s", err, out)
	}
	if !strings.Contains(out, "errata") || !strings.Contains(out, "primeira nota") || !strings.Contains(out, "segunda nota") {
		t.Fatalf("show missing errata entries:\n%s", out)
	}
	if strings.Index(out, "primeira nota") > strings.Index(out, "segunda nota") {
		t.Fatalf("errata entries out of chronological order:\n%s", out)
	}
}

func TestShowJSONIncludesErrata(t *testing.T) {
	projDir, _, _ := setupProject(t, "finance")
	if out, err := execCerne(t, "feature", "new", "Boletos", "--module", "finance", "--dir", projDir); err != nil {
		t.Fatalf("feature new: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "feature", "errata", "F-0001", "nota json", "--dir", projDir); err != nil {
		t.Fatalf("errata: %v\n%s", err, out)
	}
	out, err := execCerne(t, "show", "F-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatalf("show --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, "errata") || !strings.Contains(out, "nota json") {
		t.Fatalf("show --json missing errata:\n%s", out)
	}
}

func hasSubcommand(root *cobra.Command, parent, child string) bool {
	for _, c := range root.Commands() {
		if c.Name() != parent {
			continue
		}
		for _, sub := range c.Commands() {
			if sub.Name() == child {
				return true
			}
		}
	}
	return false
}
