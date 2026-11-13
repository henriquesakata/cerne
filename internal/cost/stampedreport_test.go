package cost

import (
	"reflect"
	"testing"
)

// F-0391 crit. 5: the costs detail shows the stamped cost as `cerne cost <id>
// --json` reads an anchor — by model and by role, the uncovered share named
// "unknown" — without reading a transcript.
func TestStampedReport(t *testing.T) {
	r := Report{ID: "F-0007", Counters: Counters{Input: 100, Output: 10}, Sidechain: Counters{Input: 40},
		ByModel:  map[string]Counters{"claude-opus-5-5": {Input: 100, Output: 10}},
		ByRole:   map[string]Counters{"builder": {Input: 60, Output: 10}, "critic": {Input: 30}},
		Messages: 5, Sessions: 2, First: "2026-11-01T10:00:00Z", Last: "2026-11-02T10:00:00Z", Unattributed: Counters{Input: 7}}
	body, err := Stamp("# F-0007\n", r)
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	got, ok := StampedReport(body)
	if !ok {
		t.Fatal("StampedReport: no anchor read")
	}
	want := Report{ID: "F-0007", Source: "stamp", Counters: r.Counters, Sidechain: r.Sidechain, ByModel: r.ByModel,
		ByRole:   map[string]Counters{"builder": {Input: 60, Output: 10}, "critic": {Input: 30}, "unknown": {Input: 10}},
		Messages: 5, Sessions: 2, First: r.First, Last: r.Last, Unattributed: r.Unattributed}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("StampedReport =\n%+v\nwant\n%+v", got, want)
	}

	old, err := Stamp("# F-0008\n", Report{ID: "F-0008", Counters: Counters{Input: 5}, ByModel: map[string]Counters{}, Messages: 1})
	if err != nil {
		t.Fatalf("Stamp: %v", err)
	}
	if got, ok := StampedReport(old); !ok || got.ByRole != nil {
		t.Errorf("pre-F-0384 anchor: ByRole = %v, ok = %v; want nil, true", got.ByRole, ok)
	}
	if _, ok := StampedReport("# F-0009\n\n## Resultado\n"); ok {
		t.Error("a result with no anchor read as stamped")
	}
}
