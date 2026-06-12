package cli

import (
	"errors"
	"os"

	"gitlab.com/rockt/cerne/internal/doccheck"
	"gitlab.com/rockt/cerne/internal/domain"
)

// hasRealContent aliases doccheck.HasRealContent; the implementation lives
// in internal/doccheck so internal/lint can share the stub detection.
var hasRealContent = doccheck.HasRealContent

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
