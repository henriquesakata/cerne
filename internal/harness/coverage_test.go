package harness

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// coverageFixture: a project with claude wired, .cursor/ detected, no
// .gemini/, and an AGENTS.md — the shape F-0259 criteria 5-6 exercise.
func coverageFixture(t *testing.T) Inventory {
	t.Helper()
	proj := t.TempDir()
	writeFile(t, filepath.Join(proj, ".claude", "settings.json"), settingsWiring("x.sh"))
	writeFile(t, filepath.Join(proj, ".claude", "hooks", "x.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(proj, ".cursor", "rules", "cerne.mdc"), "rules\n")
	writeFile(t, filepath.Join(proj, "AGENTS.md"), "# agents\n")
	return Inspect(Options{ProjDir: proj})
}

func coverageOf(t *testing.T, inv Inventory, provider string) ProviderCoverage {
	t.Helper()
	for _, c := range inv.Coverage {
		if c.Provider == provider {
			return c
		}
	}
	t.Fatalf("coverage for %s absent", provider)
	return ProviderCoverage{}
}

// TestCoveragePerProvider: totals and gap classification (criterion 5).
func TestCoveragePerProvider(t *testing.T) {
	inv := coverageFixture(t)
	if len(inv.Coverage) != 4 {
		t.Fatalf("coverage entries = %d, want 4 (all known providers)", len(inv.Coverage))
	}

	claude := coverageOf(t, inv, "claude")
	if !claude.Detected {
		t.Error("claude must always be detected")
	}
	if claude.Total == 0 {
		t.Error("claude total = 0, want > 0")
	}
	var claudeAgentsGap bool
	for _, g := range claude.Gaps {
		if g.Name == "AGENTS.md" && g.Kind == "degraded" {
			claudeAgentsGap = true
		}
	}
	if !claudeAgentsGap {
		t.Error("AGENTS.md (codex/cursor surface) missing from claude's degraded gaps")
	}

	cursor := coverageOf(t, inv, "cursor")
	if !cursor.Detected {
		t.Error("cursor not detected despite .cursor/ present")
	}
	if cursor.Total == 0 {
		t.Error("cursor total = 0, want > 0 (cerne class + AGENTS.md + parity rows)")
	}
	var cursorDegraded, cursorMissing bool
	for _, g := range cursor.Gaps {
		if g.Kind == "degraded" && g.Class == "providers" && g.Detail == "" {
			t.Errorf("degraded capability gap %q lacks the reason", g.Name)
		}
		if g.Kind == "degraded" && g.Name == "Work gate (deny unregistered edits)" {
			cursorDegraded = true
		}
		if g.Kind == "missing" && g.Class == "providers" {
			cursorMissing = true
		}
	}
	if !cursorDegraded {
		t.Error("cursor's degraded parity capability (work gate) not reported")
	}
	if cursorMissing {
		t.Error("detected provider must not report installable capabilities as missing")
	}
}

// TestCoverageUndetectedProvider: a known provider without its config dir is
// still listed, with its full installable gap (criterion 6).
func TestCoverageUndetectedProvider(t *testing.T) {
	inv := coverageFixture(t)
	gemini := coverageOf(t, inv, "gemini")
	if gemini.Detected {
		t.Error("gemini detected despite no .gemini/ dir")
	}
	var missingWithFix, degraded bool
	for _, g := range gemini.Gaps {
		if g.Kind == "missing" && g.Fix == "cerne hook install --provider gemini" {
			missingWithFix = true
		}
		if g.Kind == "degraded" {
			degraded = true
		}
	}
	if !missingWithFix {
		t.Error("undetected gemini has no missing capability with the install fix")
	}
	if !degraded {
		t.Error("gemini's degraded capabilities (per parity matrix) not reported")
	}
}

// TestPayloadCarriesConsumersAndCoverage: the serialized inventory exposes
// both fields for API/CLI consumers (criterion 4).
func TestPayloadCarriesConsumersAndCoverage(t *testing.T) {
	inv := coverageFixture(t)
	raw, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"consumers"`, `"coverage"`, `"detected"`, `"gaps"`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("payload lacks %s", field)
		}
	}
}
