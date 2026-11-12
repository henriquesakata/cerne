package app

import (
	"errors"
	"io/fs"
	"os"

	"github.com/henriquesakata/cerne/internal/lint"
	"github.com/henriquesakata/cerne/internal/project"
)

// ApproveWarnings lists what `feature approve --check` shows about feature
// id's spec without blocking the approve (F-0353): today the advisory
// spec-cli-literal-pt findings, as "rule: detail" lines, only when the project
// at projDir declares lint.cli_output_language = "en". They are findings, not
// impediments — ApproveReadiness stays the list of what stops the approve.
func ApproveWarnings(repoDir, projDir, id string) ([]string, error) {
	if project.CLIOutputLanguage(projDir) != "en" {
		return nil, nil
	}
	spec, err := os.ReadFile(FeatureDocPath(repoDir, id, "spec.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range lint.SpecCLILiteralPT(id, spec) {
		out = append(out, f.Rule+": "+f.Detail)
	}
	return out, nil
}
