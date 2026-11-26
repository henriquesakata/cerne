package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The page sends the option text it rendered, which the parser already
// returns without the suffix; the endpoint stores it as it came. This pins
// existing behavior (it passes on its first run).
// F-0517 crit. 3
func TestClarifyAnswerEndpointRecommendedOption(t *testing.T) {
	repoDir, projDir, _ := newGitBackend(t)
	raw := "[CLARIFY: which format? | json | yaml (recommended)]"
	spec := realDoc("F-0001", "Problema real descrito aqui.") + "\n" + raw + "\n"
	rec := doLocal(t, New(repoDir, projDir, ""), http.MethodPut, "/api/features/F-0001/docs/spec", spec, &struct{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("seeding spec: status=%d body=%s", rec.Code, rec.Body.String())
	}
	doc := "content/features/F-0001/spec.md"
	data, err := os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if err != nil {
		t.Fatal(err)
	}
	offset := strings.Index(string(data), raw)
	body := fmt.Sprintf(`{"doc":%q,"offset":%d,"raw":%q,"answer":"yaml"}`, doc, offset, raw)
	rec = doJSON(t, New(repoDir, projDir, ""), http.MethodPost, "/api/clarify/F-0001/answer", body, &struct{}{})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data, _ = os.ReadFile(filepath.Join(repoDir, filepath.FromSlash(doc)))
	if !strings.Contains(string(data), "[CLARIFIED: which format? → yaml]") || strings.Contains(string(data), "recommended") {
		t.Fatalf("answer not stored without the suffix:\n%s", data)
	}
}
