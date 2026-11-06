package cost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gitlab.com/rockt/cerne/internal/domain"
)

func harvest(t *testing.T, in Inputs, sess, transcript string, now time.Time) {
	t.Helper()
	if err := Harvest(in, sess, transcript, now); err != nil {
		t.Fatalf("Harvest: %v", err)
	}
}

// readSnap decodes <cache>/cost/<id>.json.
func readSnap(t *testing.T, cache, id string) snapshot {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cache, "cost", id+".json"))
	if err != nil {
		t.Fatalf("snapshot %s: %v", id, err)
	}
	var s snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("snapshot %s: %v", id, err)
	}
	return s
}

func sessDir(in Inputs, parts ...string) string {
	return filepath.Join(append([]string{in.Roots[0], "projects", "-tmp-p"}, parts...)...)
}

// prune deletes sess's transcripts (main and subagents).
func prune(t *testing.T, in Inputs, sess string) {
	t.Helper()
	if err := os.Remove(sessDir(in, sess+".jsonl")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.RemoveAll(sessDir(in, sess)); err != nil {
		t.Fatal(err)
	}
}

// TestHarvestSurvivesPrunedTranscript (clarification + crit. 11): a harvested
// snapshot keeps the cost of a session whose transcripts are then pruned.
func TestHarvestSurvivesPrunedTranscript(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 2, 3, 4)},
		tmsg{ts: at(3), id: "b", req: "r", agent: "x", model: "claude-sonnet-5", c: cc(10, 20, 30, 40)})
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	before := compute(t, in, "F-0002")

	harvest(t, in, "s", sessDir(in, "s.jsonl"), time.Now())
	if _, err := os.Stat(filepath.Join(in.CacheDir, "cost", "F-0002.json")); err != nil {
		t.Fatalf("no snapshot: %v", err)
	}
	prune(t, in, "s")

	after := compute(t, in, "F-0002")
	if after.Counters != before.Counters || after.Sidechain != before.Sidechain || !reflect.DeepEqual(after.ByModel, before.ByModel) ||
		after.Messages != before.Messages || after.First != before.First || after.Last != before.Last {
		t.Fatalf("pruned:\n%+v\nwant the live numbers:\n%+v", after, before)
	}
	if after.SessionsWithoutTranscript != 1 || after.SessionsRecovered != 1 {
		t.Fatalf("without=%d recovered=%d, want 1/1", after.SessionsWithoutTranscript, after.SessionsRecovered)
	}
}

// TestHarvestReplacesPerSessionFile: a re-harvest replaces the file's entry
// with the grown transcript — never adds to it.
func TestHarvestReplacesPerSessionFile(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	a, b := tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)}, tmsg{ts: at(3), id: "b", req: "r", c: cc(2, 2, 2, 2)}
	writeSession(t, in.Roots[0], "s", a)
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	harvest(t, in, "s", "", time.Now().Add(-time.Hour))
	writeSession(t, in.Roots[0], "s", a, b)
	harvest(t, in, "s", "", time.Now())

	if f := readSnap(t, in.CacheDir, "F-0002").Sessions["s"].Files["-tmp-p/s.jsonl"]; f == nil || f.Messages != 2 {
		t.Fatalf("snapshot entry = %+v, want 2 messages", f)
	}
	prune(t, in, "s")
	if r := compute(t, in, "F-0002"); r.Counters != cc(3, 3, 3, 3) || r.Messages != 2 {
		t.Fatalf("report = %+v, want a+b once", r)
	}
}

// TestHarvestKeepsPrunedFileEntry: a file pruned between harvests keeps its
// entry; the files re-read are updated.
func TestHarvestKeepsPrunedFileEntry(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	a, b, c := tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 0, 0, 0)},
		tmsg{ts: at(3), id: "b", req: "r", agent: "x", c: cc(0, 1, 0, 0)},
		tmsg{ts: at(4), id: "c", req: "r", c: cc(0, 0, 1, 0)}
	writeSession(t, in.Roots[0], "s", a, b)
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	harvest(t, in, "s", "", time.Now().Add(-time.Hour))
	if err := os.RemoveAll(sessDir(in, "s")); err != nil {
		t.Fatal(err)
	}
	writeSession(t, in.Roots[0], "s", a, c)
	harvest(t, in, "s", "", time.Now())

	files := readSnap(t, in.CacheDir, "F-0002").Sessions["s"].Files
	if sub := files["-tmp-p/s/subagents/agent-x.jsonl"]; sub == nil || sub.Messages != 1 {
		t.Fatalf("pruned subagent entry = %+v, want kept with 1 message", sub)
	}
	if m := files["-tmp-p/s.jsonl"]; m == nil || m.Messages != 2 {
		t.Fatalf("main entry = %+v, want 2 messages", m)
	}
	prune(t, in, "s")
	if r := compute(t, in, "F-0002"); r.Counters != cc(1, 1, 1, 0) || r.Sidechain != cc(0, 1, 0, 0) {
		t.Fatalf("report = %+v, want a+b+c with b as sidechain", r)
	}
}

// TestHarvestSkipsClosedFeature: no snapshot for a feature past
// TitleEditable, for a session outside the ledger, or under another harness.
func TestHarvestSkipsClosedFeature(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateImplemented)
	saveFeature(t, in.RepoDir, "F-0003", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s", tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)})
	writeSession(t, in.Roots[0], "s3", tmsg{ts: at(2), id: "b", req: "r", c: cc(1, 1, 1, 1)})
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	if err := AppendBind(in.CacheDir, Bind{Session: "s3", Entity: "F-0003", Harness: "other", TS: at(1)}); err != nil {
		t.Fatal(err)
	}
	for _, sess := range []string{"s", "nobody", "s3"} {
		harvest(t, in, sess, "", time.Now())
	}
	if entries, _ := os.ReadDir(filepath.Join(in.CacheDir, "cost")); len(entries) != 0 {
		t.Fatalf("snapshots written: %v", entries)
	}
}

// TestHarvestSkipsUnchangedFiles: a file untouched since the session's last
// harvest is not re-read.
func TestHarvestSkipsUnchangedFiles(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	writeSession(t, in.Roots[0], "s",
		tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 1, 1, 1)},
		tmsg{ts: at(3), id: "b", req: "r", agent: "x", c: cc(2, 2, 2, 2)})
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	first := time.Now()
	harvest(t, in, "s", "", first)
	before := readSnap(t, in.CacheDir, "F-0002").Sessions["s"].Files

	old := first.Add(-time.Hour)
	sub := sessDir(in, "s", "subagents", "agent-x.jsonl")
	if err := os.WriteFile(sub, []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{sub, sessDir(in, "s.jsonl")} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	harvest(t, in, "s", "", time.Now())
	if after := readSnap(t, in.CacheDir, "F-0002").Sessions["s"].Files; !reflect.DeepEqual(after, before) {
		t.Fatalf("unchanged files re-read:\n%+v\nwant\n%+v", after, before)
	}
}

// TestComputeLiveWinsUnlessSnapshotHasMore: per file, the snapshot replaces
// the live read only when it saw more messages; never both summed.
func TestComputeLiveWinsUnlessSnapshotHasMore(t *testing.T) {
	in := fixture(t)
	saveFeature(t, in.RepoDir, "F-0002", domain.StateInDevelopment)
	a, b, c := tmsg{ts: at(2), id: "a", req: "r", c: cc(1, 0, 0, 0)},
		tmsg{ts: at(3), id: "b", req: "r", c: cc(0, 1, 0, 0)},
		tmsg{ts: at(4), id: "c", req: "r", c: cc(0, 0, 1, 0)}
	writeSession(t, in.Roots[0], "s", a, b, c)
	bindAt(t, in.CacheDir, "s", "", "F-0002", at(1))
	harvest(t, in, "s", "", time.Now())

	writeSession(t, in.Roots[0], "s", a, b)
	if r := compute(t, in, "F-0002"); r.Counters != cc(1, 1, 1, 0) || r.SessionsRecovered != 0 {
		t.Fatalf("snapshot with more messages lost: %+v", r)
	}
	c.c = cc(0, 0, 5, 0)
	writeSession(t, in.Roots[0], "s", a, b, c)
	if r := compute(t, in, "F-0002"); r.Counters != cc(1, 1, 5, 0) {
		t.Fatalf("live with as many messages lost: %+v", r)
	}
}
