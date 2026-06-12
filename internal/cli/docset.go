package cli

import (
	"errors"
	"os"
	"strings"

	"gitlab.com/rockt/cerne/internal/domain"
)

// stubPrefixes are the placeholder lines cerne writes into freshly created
// lifecycle documents; a document whose body is only these is still a stub.
var stubPrefixes = []string{"> Spec pendente", "> Plano pendente", "> Resultado pendente"}

// featureDocSet reports which lifecycle documents of the feature exist in
// the backend with real (non-stub) content.
func featureDocSet(repoDir, id string) (domain.DocSet, error) {
	var ds domain.DocSet
	var err error
	if ds.HasSpec, err = isRealDoc(featureDocPath(repoDir, id, "spec.md")); err != nil {
		return ds, err
	}
	if ds.HasPlan, err = isRealDoc(featureDocPath(repoDir, id, "plan.md")); err != nil {
		return ds, err
	}
	if ds.HasResult, err = isRealDoc(featureDocPath(repoDir, id, "result.md")); err != nil {
		return ds, err
	}
	return ds, nil
}

// isRealDoc reports whether the file exists and has real content.
func isRealDoc(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return hasRealContent(data), nil
}

// hasRealContent reports whether a lifecycle document carries content beyond
// its stub: ignoring the leading frontmatter block, blank lines, the first
// "#" heading and stub placeholder lines, at least one line remains.
func hasRealContent(data []byte) bool {
	lines := strings.Split(string(data), "\n")
	// Skip a leading frontmatter block delimited by "---" lines.
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	headingSeen := false
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !headingSeen && strings.HasPrefix(trimmed, "#") {
			headingSeen = true
			continue
		}
		isStub := false
		for _, p := range stubPrefixes {
			if strings.HasPrefix(trimmed, p) {
				isStub = true
				break
			}
		}
		if !isStub {
			return true
		}
	}
	return false
}
