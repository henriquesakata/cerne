package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

func logProject(t *testing.T) (projDir, repoDir string) {
	t.Helper()
	t.Setenv("CERNE_ACTOR", "")
	t.Setenv("USER", "tester")
	projDir, _, repoDir = setupProject(t)
	if out, err := execCerne(t, "work", "start", "Trabalho", "--dir", projDir); err != nil {
		t.Fatalf("work start: %v\n%s", err, out)
	}
	return projDir, repoDir
}

func TestWorkLogPrintsLineAndWrites(t *testing.T) {
	projDir, repoDir := logProject(t)
	out, err := execCerne(t, "work", "log", "W-0001", "Sim, mergeia", "--actor", "owner", "--dir", projDir)
	if err != nil {
		t.Fatalf("work log: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "logged #1 on W-0001 at ") || !strings.Contains(out, "Z)") {
		t.Fatalf("line = %q", out)
	}
	es, _ := store.LoadLog(repoDir, "W-0001")
	if len(es) != 1 || es[0].Text != "Sim, mergeia" || es[0].Actor != "owner" {
		t.Fatalf("log = %+v", es)
	}
}

func TestWorkLogTextFromStdinAndRefusals(t *testing.T) {
	projDir, repoDir := logProject(t)
	out, err := execCerneStdin(t, "pode aplicar em prod\n", "work", "log", "W-0001", "--dir", projDir)
	if err != nil || !strings.Contains(out, "logged #1") {
		t.Fatalf("stdin: %v\n%s", err, out)
	}
	if out, err := execCerne(t, "work", "log", "W-0001", "x", "--corrects", "9", "--dir", projDir); err == nil {
		t.Fatalf("corrects missing entry accepted:\n%s", out)
	}
	if out, err := execCerne(t, "work", "log", "W-0001", "mail ana@exemplo.com.br", "--dir", projDir); err == nil {
		t.Fatalf("e-mail accepted:\n%s", out)
	}
	if es, _ := store.LoadLog(repoDir, "W-0001"); len(es) != 1 {
		t.Fatalf("refusals wrote: %+v", es)
	}
}

func TestShowWorkListsLog(t *testing.T) {
	projDir, _ := logProject(t)
	for _, a := range [][]string{
		{"um", "--actor", "owner"},
		{"dois", "--corrects", "1"},
	} {
		args := append([]string{"work", "log", "W-0001"}, a...)
		if out, err := execCerne(t, append(args, "--dir", projDir)...); err != nil {
			t.Fatalf("%v: %v\n%s", a, err, out)
		}
	}
	out, err := execCerne(t, "show", "W-0001", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## log\n") || !strings.Contains(out, "#1  ") || !strings.Contains(out, "  owner (recorded by ") || !strings.Contains(out, "dois (corrects #1)") {
		t.Fatalf("show text:\n%s", out)
	}
	js, err := execCerne(t, "show", "W-0001", "--json", "--dir", projDir)
	if err != nil {
		t.Fatal(err)
	}
	var j struct {
		Log     []store.LogEntry        `json:"log"`
		Content []struct{ Name string } `json:"content"`
	}
	if err := json.Unmarshal([]byte(js), &j); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	if len(j.Log) != 2 || j.Log[1].Corrects != 1 || j.Log[0].RecordedBy == "" {
		t.Fatalf("json log = %+v", j.Log)
	}
	for _, c := range j.Content {
		if c.Name == "log" {
			t.Fatalf("log duplicated in content")
		}
	}
}
