package importer

import (
	"errors"
	"testing"
)

// validItem returns a minimal valid PlanItem of the given kind for table tests.
func validItem(kind string) PlanItem {
	it := PlanItem{Kind: kind, Title: "T", Body: "body", Source: kind + ".md"}
	switch kind {
	case "feature":
		it.State = "implemented"
	case "decision":
		it.State = "active"
	case "reference":
		it.State = "current"
		it.URL = "https://example.com"
	case "runbook":
		it.State = "active"
	case "note":
		it.State = "active"
	}
	return it
}

func TestValidatePlanAccepts(t *testing.T) {
	p := Plan{Items: []PlanItem{
		validItem("feature"), validItem("decision"), validItem("reference"),
		validItem("runbook"), validItem("note"),
	}}
	if err := ValidatePlan(p); err != nil {
		t.Fatalf("ValidatePlan(valid 5-kind plan) = %v, want nil", err)
	}
}

func TestValidatePlanRejects(t *testing.T) {
	refNoURL := validItem("reference")
	refNoURL.URL = ""

	bad := func(mut func(*PlanItem)) PlanItem {
		it := validItem("feature")
		mut(&it)
		return it
	}
	tests := []struct {
		name string
		item PlanItem
	}{
		{"unknown kind", bad(func(i *PlanItem) { i.Kind = "widget" })},
		{"bad state for kind", bad(func(i *PlanItem) { i.State = "frobnicated" })},
		{"empty title", bad(func(i *PlanItem) { i.Title = "" })},
		{"empty source", bad(func(i *PlanItem) { i.Source = "" })},
		{"blank body", bad(func(i *PlanItem) { i.Body = "   " })},
		{"reference without url", refNoURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePlan(Plan{Items: []PlanItem{tt.item}})
			if !errors.Is(err, ErrInvalidPlan) {
				t.Fatalf("ValidatePlan(%s) = %v, want ErrInvalidPlan", tt.name, err)
			}
		})
	}
}

func TestValidatePlanEmptyRejected(t *testing.T) {
	if err := ValidatePlan(Plan{}); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("ValidatePlan(empty) = %v, want ErrInvalidPlan", err)
	}
}

func TestParsePlanRoundTrip(t *testing.T) {
	data := []byte(`{"items":[{"kind":"note","title":"N","state":"active","body":"b","source":"CLAUDE.md","informs":["F-0001"]}]}`)
	p, err := ParsePlan(data)
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if len(p.Items) != 1 || p.Items[0].Kind != "note" || len(p.Items[0].Informs) != 1 || p.Items[0].Informs[0] != "F-0001" {
		t.Fatalf("ParsePlan = %+v", p)
	}
}

func TestParsePlanInvalidJSON(t *testing.T) {
	if _, err := ParsePlan([]byte("not json")); err == nil {
		t.Fatal("ParsePlan(garbage) = nil err, want error")
	}
}
