package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIdeaLegacyText(t *testing.T) {
	cases := []struct {
		name, json, title, body string
		legacy                  bool
	}{
		{"legacy", `{"id":"I-0001","text":"\n  primeira linha\nresto","state":"active"}`, "primeira linha", "resto", true},
		{"legacy one line", `{"id":"I-0001","text":"so isto","state":"active"}`, "so isto", "", true},
		{"new", `{"id":"I-0001","title":"t","body":"b","state":"active"}`, "t", "b", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			sub := filepath.Join(dir, "entities", "ideas")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, "I-0001.json"), []byte(c.json), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := LoadIdea(dir, "I-0001")
			if err != nil || got.Title != c.title || got.Body != c.body || got.Legacy() != c.legacy {
				t.Fatalf("got %+v legacy=%v err=%v, want %q/%q legacy=%v", got, got.Legacy(), err, c.title, c.body, c.legacy)
			}
			all, err := LoadAllIdeas(dir)
			if err != nil || len(all) != 1 || all[0].Title != c.title || all[0].Legacy() != c.legacy {
				t.Fatalf("LoadAllIdeas = %+v, %v", all, err)
			}
		})
	}
}
