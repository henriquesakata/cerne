package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// execGuide runs the command from a bare temp dir: it must need no project.
func execGuide(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Chdir(t.TempDir())
	return execCerne(t, append([]string{"guide"}, args...)...)
}

func TestGuideTextAnswer(t *testing.T) {
	out, err := execGuide(t, "how do I change a project setting")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "## ") || !strings.Contains(out, "(faq)") || !strings.Contains(out, "cerne settings set") {
		t.Fatalf("output:\n%s", out)
	}
	if n := strings.Count(out, "\n## ") + 1; n > 3 {
		t.Errorf("%d sections, want at most 3", n)
	}
}

func TestGuideLimit(t *testing.T) {
	out, err := execGuide(t, "--limit", "1", "settings")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "## ") != 1 {
		t.Fatalf("want 1 section:\n%s", out)
	}
}

func TestGuideJSON(t *testing.T) {
	out, err := execGuide(t, "--json", "change a project setting")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Query    string `json:"query"`
		Sections []struct{ Title, Source, Text string }
	}
	if err := json.NewDecoder(bytes.NewBufferString(out)).Decode(&got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Query != "change a project setting" || len(got.Sections) == 0 || got.Sections[0].Source == "" || got.Sections[0].Text == "" {
		t.Fatalf("json = %+v", got)
	}
}

func TestGuideNoMatchListsTopics(t *testing.T) {
	out, err := execGuide(t, "zzqxj")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "No guide section matches \"zzqxj\".\n") || !strings.Contains(out, "Topics") ||
		!strings.Contains(out, "Feature loop") || !strings.Contains(out, "How do I change a project setting") {
		t.Fatalf("output:\n%s", out)
	}
}

// TestGuideNoQuestionPrintsTheCatalog pins F-0399 adjustment 3: with no
// question the guide prints what cerne can do, grouped, from the command tree;
// the topics stay one flag away.
func TestGuideNoQuestionPrintsTheCatalog(t *testing.T) {
	out, err := execGuide(t)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"\nfeature  Create and manage features", "  cerne feature approve  ", "\nroom  "} {
		if !strings.Contains(out, want) {
			t.Errorf("catalog lacks %q", want)
		}
	}
	if strings.Contains(out, "No guide section") || strings.Contains(out, "Topics (") {
		t.Errorf("catalog printed the topics:\n%s", out)
	}
	if !strings.HasSuffix(out, "ask: cerne guide \"<feature>\" for how to use it\n") {
		t.Errorf("catalog does not end with the ask line:\n%s", out[max(0, len(out)-200):])
	}
	topics, err := execGuide(t, "--topics")
	if err != nil || !strings.Contains(topics, "Topics (") || !strings.Contains(topics, "Feature loop") {
		t.Fatalf("--topics: %v\n%s", err, topics)
	}
	j, err := execGuide(t, "--json")
	if err != nil || !strings.Contains(j, `"sections": []`) {
		t.Fatalf("empty json: %v\n%s", err, j)
	}
}

func TestGuideStopwordQueryListsTopics(t *testing.T) {
	out, err := execGuide(t, "the")
	if err != nil || !strings.HasPrefix(out, "No guide section matches \"the\".") || !strings.Contains(out, "Topics") {
		t.Fatalf("err=%v output:\n%s", err, out)
	}
}

func TestGuideRefusesLimitBelowOne(t *testing.T) {
	for _, l := range []string{"0", "-1"} {
		if _, err := execGuide(t, "--limit", l, "settings"); err == nil || !strings.Contains(err.Error(), "--limit") {
			t.Errorf("--limit %s: err = %v, want a refusal naming --limit", l, err)
		}
	}
}

// TestGuideWhatCanCerneDoPointsToTheCatalog: the question an agent asks first
// is answered by the FAQ, which names the no-question call (F-0399).
func TestGuideWhatCanCerneDoPointsToTheCatalog(t *testing.T) {
	out, err := execGuide(t, "--limit", "1", "what can cerne do")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "(faq)") || !strings.Contains(out, "`cerne guide`") || !strings.Contains(out, "--topics") {
		t.Fatalf("first section is not the catalog answer:\n%s", out)
	}
}
