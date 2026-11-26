package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// decided seeds feature id in state with a feature.completed event carrying
// the risk gate's decision payload (as F-0516 records it) and commits.
func decided(t *testing.T, repoDir, id string, state domain.FeatureState, facets []string, payload map[string]string) {
	t.Helper()
	if err := store.SaveFeature(repoDir, store.Feature{ID: id, Title: "Card " + id, State: state, Facets: facets}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendEvent(repoDir, domain.NewEvent(id, "feature.completed", payload, "agent")); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed "+id); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatal(err)
	}
}

// lowPayload and heldPayload are a decision of the risk gate: accepted as
// low risk, and held on go.mod.
func lowPayload() map[string]string {
	return map[string]string{"risk": "low", "risk_area": "touches no sensitive path or facet", "risk_verdict": "win", "risk_criteria_tested": "2/2",
		"risk_files": "internal/x/x.go, internal/x/x_test.go", "risk_paths": "", "risk_facets": "", "risk_base": "b1", "risk_head": "h1"}
}

func heldPayload() map[string]string {
	return map[string]string{"risk": "held", "risk_area": "touches a sensitive path: go.mod", "risk_verdict": "win", "risk_criteria_tested": "2/2",
		"risk_files": "go.mod, internal/x/x.go", "risk_paths": "go.mod", "risk_patterns": "go.mod", "risk_facets": "", "risk_base": "b2", "risk_head": "h2"}
}

// TestFeedbackEventPayload: the feedback is an event of the card with the
// answer, the reason and the measurements the gate recorded on its decision.
// F-0518 crit. 4
func TestFeedbackEventPayload(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	if _, err := RecordRiskFeedback(repoDir, projDir, "F-0001", RiskFeedback{Answer: FeedbackShouldHaveAsked, Reason: "touched the parser"}); err != nil {
		t.Fatal(err)
	}
	p := lastEntityEvent(t, repoDir, "F-0001", riskFeedbackEvent).Payload
	for k, want := range map[string]string{"answer": FeedbackShouldHaveAsked, "reason": "touched the parser", "risk": "low", "risk_verdict": "win", "risk_criteria_tested": "2/2", "risk_head": "h1", "risk_files": "internal/x/x.go, internal/x/x_test.go"} {
		if p[k] != want {
			t.Errorf("payload[%s] = %q, want %q (payload %v)", k, p[k], want, p)
		}
	}
}

// TestFeedbackReplacesPrevious: a second answer on the same card is the one
// that counts; the first stays in the log.
// F-0518 crit. 4
func TestFeedbackReplacesPrevious(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, heldPayload())
	for _, reason := range []string{"first", "second"} {
		if _, err := RecordRiskFeedback(repoDir, projDir, "F-0001", RiskFeedback{Answer: FeedbackNotNeeded, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	if evs := entityEvents(t, repoDir, "F-0001", riskFeedbackEvent); len(evs) != 2 {
		t.Fatalf("%d feedback events, want both kept in the log", len(evs))
	}
	d, err := RiskDecisionOf(repoDir, projDir, "F-0001")
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.Feedback == nil || d.Feedback.Reason != "second" {
		t.Fatalf("decision %+v: want the second answer as the one that counts", d)
	}
}

// TestFeedbackMatchesTheDecision: a card the gate never decided, and an
// answer that does not match what the gate decided, are refused with a
// sentinel that names the decision; nothing is written.
// F-0518 crit. 2
func TestFeedbackMatchesTheDecision(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	saveGates(t, projDir, project.GatesConfig{Accept: project.GateRisk})
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	decided(t, repoDir, "F-0002", domain.StateDone, []string{"code"}, heldPayload())
	decided(t, repoDir, "F-0003", domain.StateDone, []string{"code"}, map[string]string{"by": "x"})
	for _, c := range []struct {
		id, answer string
		want       error
		says       string
	}{
		{"F-0001", FeedbackNotNeeded, ErrFeedbackMismatch, "accepted as low risk"},
		{"F-0002", FeedbackShouldHaveAsked, ErrFeedbackMismatch, "held"},
		{"F-0003", FeedbackNotNeeded, ErrNoRiskDecision, "risk gate"},
	} {
		_, err := RecordRiskFeedback(repoDir, projDir, c.id, RiskFeedback{Answer: c.answer})
		if !errors.Is(err, c.want) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s %s: err = %v, want %v naming %q", c.id, c.answer, err, c.want, c.says)
		}
		if evs := entityEvents(t, repoDir, c.id, riskFeedbackEvent); len(evs) != 0 {
			t.Errorf("%s: a refused answer wrote %d events", c.id, len(evs))
		}
	}
}

// calibrating stores the risk gate with a calibration of n answers.
func calibrating(t *testing.T, projDir string, n int64) {
	t.Helper()
	saveGates(t, projDir, calibration(n))
}

func calibration(n int64) project.GatesConfig {
	return project.GatesConfig{Accept: project.GateRisk, RiskCalibrationN: &n}
}

// answered appends an owner's answer on each of ids, as RecordRiskFeedback
// writes it.
func answered(t *testing.T, repoDir string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := store.AppendEvent(repoDir, domain.NewEvent(id, riskFeedbackEvent, map[string]string{"answer": FeedbackNotNeeded, "risk": "held"}, "human")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := gitback.CommitAndPush(repoDir, "answers"); err != nil && !errors.Is(err, gitback.ErrNothingToCommit) {
		t.Fatal(err)
	}
}

// TestRiskCalibrationHoldsAndRecords: until the owner has answered as many
// cards as gates.risk_calibration, the gate holds a low-risk delivery and
// records what it would have decided; a card answered twice counts once and
// a decided card with no answer not at all; at the number, and with
// calibration at zero, it decides as F-0516; the owner's auto mark passes in
// calibration too.
// F-0518 crit. 12, 15
func TestRiskCalibrationHoldsAndRecords(t *testing.T) {
	for _, c := range []struct {
		name     string
		target   int64
		answers  []string
		mode     string
		low      bool
		progress string
	}{
		{"before the number", 2, nil, "", false, "0/2"},
		{"one card answered twice", 2, []string{"F-0008", "F-0008"}, "", false, "1/2"},
		{"at the number", 2, []string{"F-0008", "F-0009"}, "", true, ""},
		{"calibration off", 0, nil, "", true, ""},
		{"marked auto in calibration", 2, nil, AcceptModeAuto, true, "0/2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			asActor(t, "agent")
			repoDir, projDir, _ := newFeatureBackend(t)
			riskDeliveryUnder(t, calibration(c.target), repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, AcceptMode: c.mode}, "vitória", cleanChange())
			// A card the gate decided in calibration that nobody answered.
			decided(t, repoDir, "F-0007", domain.StateDone, []string{"code"}, map[string]string{"risk": "held", "risk_calibration": "0/2", "risk_would": "low"})
			answered(t, repoDir, c.answers...)
			out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if out.Risk == nil || out.Risk.Low() != c.low || (out.To == domain.StateDone) != c.low {
				t.Fatalf("complete = %s, risk %+v; want low %v", out.To, out.Risk, c.low)
			}
			p := lastEntityEvent(t, repoDir, "F-0001", "feature.completed").Payload
			if p["risk_calibration"] != c.progress {
				t.Errorf("risk_calibration = %q, want %q", p["risk_calibration"], c.progress)
			}
			if wantWould := map[bool]string{true: "low", false: ""}[c.progress != ""]; p["risk_would"] != wantWould {
				t.Errorf("risk_would = %q, want %q", p["risk_would"], wantWould)
			}
			if !c.low && !strings.Contains(strings.Join(out.Risk.Held(), "; "), "calibration") {
				t.Errorf("held lines %q do not name the calibration", out.Risk.Held())
			}
		})
	}
}

// TestCalibrationWouldReadsTheTaskHold: what calibration records the gate
// would have decided reads the same holds as the decision: a held
// implemented task makes it held; tasks that pass on their own leave it low.
// F-0518 crit. 12
func TestCalibrationWouldReadsTheTaskHold(t *testing.T) {
	for _, c := range []struct {
		name  string
		facet string // of T-0003
		would string
	}{{"a task held", "auth", "held"}, {"every task passes", "code", "low"}} {
		t.Run(c.name, func(t *testing.T) {
			asActor(t, "agent")
			repoDir, projDir, _ := newFeatureBackend(t)
			riskDeliveryUnder(t, calibration(2), repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", cleanChange())
			seedRiskTasks(t, repoDir, projDir, store.Feature{ID: "T-0002", Facets: []string{"code"}}, store.Feature{ID: "T-0003", Facets: []string{c.facet}})
			out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if out.To != domain.StateImplemented {
				t.Fatalf("complete in calibration = %s, want held", out.To)
			}
			if p := lastEntityEvent(t, repoDir, "F-0001", "feature.completed").Payload; p["risk_would"] != c.would || p["risk_calibration"] != "0/2" {
				t.Errorf("risk_would %q, risk_calibration %q; want %s, 0/2 (tasks %q)", p["risk_would"], p["risk_calibration"], c.would, p["risk_tasks"])
			}
		})
	}
}

// TestCalibrationStopsAsking: once the owner has given the number of answers,
// or with calibration off, the gate asks no more: no question in the view,
// and the card's decision says the calibration is over.
// F-0518 crit. 12
func TestCalibrationStopsAsking(t *testing.T) {
	for _, c := range []struct {
		name   string
		target int64
		asks   bool
	}{{"still calibrating", 2, true}, {"number reached", 1, false}, {"calibration off", 0, false}} {
		t.Run(c.name, func(t *testing.T) {
			asActor(t, "")
			repoDir, projDir, _ := newFeatureBackend(t)
			calibrating(t, projDir, c.target)
			held := map[string]string{"risk": "held", "risk_calibration": "0/2", "risk_would": "low"}
			decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, held)
			decided(t, repoDir, "F-0002", domain.StateDone, []string{"code"}, held)
			answered(t, repoDir, "F-0002")
			v, err := ReadRiskGate(repoDir, projDir, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if asks := len(v.Questions) > 0; asks != c.asks {
				t.Errorf("questions = %v, want asking %v", v.Questions, c.asks)
			}
			d, err := RiskDecisionOf(repoDir, projDir, "F-0001")
			if err != nil || d == nil || d.Calibrating != c.asks {
				t.Errorf("decision %+v (%v): want calibrating %v", d, err, c.asks)
			}
		})
	}
}

// TestRiskTasksRideByOneRule: the parent's check and the cascade judge a task
// by the same rule. Under an auto parent in calibration, an unmarked task that
// passes on its own measurements goes to done with the parent — it is not
// left implemented with a risk_held — and one the gate would hold holds both.
// F-0518 crit. 15
func TestRiskTasksRideByOneRule(t *testing.T) {
	for _, c := range []struct {
		name, facet string
		done        bool
	}{{"a task that passes", "code", true}, {"a task held", "auth", false}} {
		t.Run(c.name, func(t *testing.T) {
			asActor(t, "agent")
			repoDir, projDir, _ := newFeatureBackend(t)
			riskDeliveryUnder(t, calibration(2), repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, AcceptMode: AcceptModeAuto}, "vitória", cleanChange())
			seedRiskTasks(t, repoDir, projDir, store.Feature{ID: "T-0002", Facets: []string{c.facet}})
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
				t.Fatal(err)
			}
			parent, task := reload(t, repoDir, "F-0001"), reload(t, repoDir, "T-0002")
			if (parent.State == domain.StateDone) != c.done || (task.State == domain.StateDone) != c.done {
				t.Fatalf("parent %s, task %s; want both done %v", parent.State, task.State, c.done)
			}
			if held := entityEvents(t, repoDir, "T-0002", "feature.risk_held"); c.done && len(held) > 0 {
				t.Errorf("the task rode its parent and still got a risk_held: %v", held[0].Payload)
			}
			if c.done {
				if p := lastEntityEvent(t, repoDir, "T-0002", "feature.accepted").Payload; p["risk"] != "low" || p["via"] != "risk" {
					t.Errorf("task accept payload risk %q via %q, want low, risk", p["risk"], p["via"])
				}
			}
		})
	}
}

// TestCalibrationAnswerIsFeedback: on a card held in calibration, either
// answer is the owner's feedback, recorded with what the gate would have
// decided, and it counts toward the number.
// F-0518 crit. 12
func TestCalibrationAnswerIsFeedback(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	calibrating(t, projDir, 2)
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, map[string]string{"risk": "held", "risk_calibration": "0/2", "risk_would": "low"})
	decided(t, repoDir, "F-0002", domain.StateDone, []string{"code"}, map[string]string{"risk": "held", "risk_calibration": "0/2", "risk_would": "held"})
	for id, answer := range map[string]string{"F-0001": FeedbackShouldHaveAsked, "F-0002": FeedbackNotNeeded} {
		if _, err := RecordRiskFeedback(repoDir, projDir, id, RiskFeedback{Answer: answer}); err != nil {
			t.Fatalf("%s %s: %v", id, answer, err)
		}
		if p := lastEntityEvent(t, repoDir, id, riskFeedbackEvent).Payload; p["answer"] != answer || p["risk_would"] == "" || p["risk_calibration"] != "0/2" {
			t.Errorf("%s: payload %v, want the answer with what the gate would have decided", id, p)
		}
	}
	v, err := ReadRiskGate(repoDir, projDir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if v.Calibration.Target != 2 || v.Calibration.Answers != 2 || v.Calibration.Remaining != 0 {
		t.Errorf("calibration = %+v, want 2 of 2 answered", v.Calibration)
	}
}

// TestRiskRecordsTheDeliveryFiles: the decision names every file the
// delivery changed and the sensitive patterns that hit, so the owner can
// mark folders later; the decision groups the files by folder.
// F-0518 crit. 5
func TestRiskRecordsTheDeliveryFiles(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	change := cleanChange()
	change["go.mod"] = "module x\n"
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}}, "vitória", change)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatal(err)
	}
	p := lastEntityEvent(t, repoDir, "F-0001", "feature.completed").Payload
	if p["risk_files"] != "go.mod, internal/x/x.go, internal/x/x_test.go" || p["risk_patterns"] != "go.mod" {
		t.Errorf("risk_files %q, risk_patterns %q", p["risk_files"], p["risk_patterns"])
	}
	d, err := RiskDecisionOf(repoDir, projDir, "F-0001")
	if err != nil || d == nil || strings.Join(d.Dirs, " ") != "go.mod internal/x/**" {
		t.Fatalf("decision %+v (%v): want the folders go.mod and internal/x/**", d, err)
	}
}

// TestAcceptModeAutoStillMeasures: the auto mark waives the sensitivity
// judgment, never the measurement: uncommitted changes to tracked files,
// untracked files and a diff git cannot read still hold the card.
// F-0518 crit. 15
func TestAcceptModeAutoStillMeasures(t *testing.T) {
	for _, c := range []struct {
		name string
		mess func(t *testing.T, projDir string)
		want string
	}{
		{"a tracked file changed", func(t *testing.T, projDir string) { riskWrite(t, projDir, "base.txt", "changed\n") }, "uncommitted"},
		{"an untracked file", func(t *testing.T, projDir string) { riskWrite(t, projDir, "notes.txt", "x\n") }, "notes.txt"},
		{"no git", func(t *testing.T, projDir string) {
			if err := os.RemoveAll(filepath.Join(projDir, ".git")); err != nil {
				t.Fatal(err)
			}
		}, "cannot be read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			repoDir, projDir := t.TempDir(), t.TempDir()
			calibrating(t, projDir, 0)
			riskRepo(t, projDir, nil, cleanChange())
			riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "spec.md"), riskSpec)
			riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "result.md"), riskResult("vitória"))
			c.mess(t, projDir)
			ra := assessRisk(repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, AcceptMode: AcceptModeAuto})
			if ra.Low() || ra.Area.Holds || !strings.Contains(ra.Area.Found, c.want) {
				t.Fatalf("auto with %s: low %v, area %+v; want held, naming %q", c.name, ra.Low(), ra.Area, c.want)
			}
		})
	}
}

// TestAcceptModeAutoNeverPassesTheGateFiles: a change to the gate's own files
// — .cernerc, the agents' configs and cerne's hooks — never passes alone, not
// under the owner's auto mark and not with the sensitive lists emptied: an
// agent's delivery could otherwise loosen the gate and be accepted by it.
// Tech-lead decision against crit. 15's letter (nothing loosens without the
// owner).
// F-0518 crit. 15
func TestAcceptModeAutoNeverPassesTheGateFiles(t *testing.T) {
	for _, file := range []string{".cernerc", ".claude/settings.json", "sub/.cursor/rules.md", "AGENTS.md", ".github/hooks/hook.sh", ".mcp.json"} {
		t.Run(file, func(t *testing.T) {
			repoDir, projDir := t.TempDir(), t.TempDir()
			g := calibration(0)
			g.RiskPaths, g.RiskFacets = []string{}, []string{} // the owner emptied both lists
			saveGates(t, projDir, g)
			rc, err := os.ReadFile(filepath.Join(projDir, ".cernerc"))
			if err != nil {
				t.Fatal(err)
			}
			change := cleanChange()
			change[file] = "# changed\n"
			if file == ".cernerc" {
				change[file] = string(rc) + "# changed\n"
			}
			riskRepo(t, projDir, nil, change)
			riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "spec.md"), riskSpec)
			riskWrite(t, repoDir, filepath.Join("content", "features", "F-0001", "result.md"), riskResult("vitória"))
			ra := assessRisk(repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, AcceptMode: AcceptModeAuto})
			if ra.Low() || ra.Area.Holds || !strings.Contains(ra.Area.Found, file) {
				t.Fatalf("auto, touching %s: low %v, area %+v; want held, naming it", file, ra.Low(), ra.Area)
			}
		})
	}
}

// TestShouldHaveAskedIsAllOrNothing: when the answer cannot be recorded after
// the lists were written, the lists go back as they were — no half-applied
// tightening.
// F-0518 crit. 5
func TestShouldHaveAskedIsAllOrNothing(t *testing.T) {
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	calibrating(t, projDir, 0)
	decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
	rc := filepath.Join(projDir, ".cernerc")
	before, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	// The log refuses the write: the answer fails after the lists.
	logs, _ := filepath.Glob(filepath.Join(repoDir, "events", "*.ndjson"))
	for _, l := range logs {
		if err := os.Chmod(l, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(l, 0o644) })
	}
	if _, err := RecordRiskFeedback(repoDir, projDir, "F-0001", RiskFeedback{Answer: FeedbackShouldHaveAsked, AddPaths: []string{"internal/x/**"}, AddFacets: []string{"code"}}); err == nil {
		t.Fatal("the answer was recorded on a log that refuses writes")
	}
	if after, _ := os.ReadFile(rc); string(after) != string(before) {
		t.Errorf("the lists stayed half-applied:\n%s", after)
	}
}

// TestRiskCapsTheFileLists: the file lists copied into events keep at most
// 200 entries each, with the total beside them, so a huge delivery does not
// grow every event; a short list is whole.
// F-0518 crit. 5
func TestRiskCapsTheFileLists(t *testing.T) {
	var files []string
	for i := range 250 {
		files = append(files, fmt.Sprintf("pkg/f%03d.go", i))
	}
	p := RiskAssessment{Files: files, Paths: files}.payload()
	for _, k := range []string{"risk_files", "risk_paths"} {
		if n := len(splitList(p[k])); n != 200 || p[k+"_total"] != "250" {
			t.Errorf("%s: %d entries, total %q; want 200 and 250", k, n, p[k+"_total"])
		}
	}
	if p := (RiskAssessment{Files: files[:3]}).payload(); len(splitList(p["risk_files"])) != 3 || p["risk_files_total"] != "3" {
		t.Errorf("a short list: %q, total %q", p["risk_files"], p["risk_files_total"])
	}
}

// TestShouldHaveAskedAddsToList: with should-have-asked the owner marks the
// paths and facets that from now on need a person, and they enter the lists
// at once; marking nothing is allowed and leaves the lists alone. Marks ride
// only that answer.
// F-0518 crit. 5
func TestShouldHaveAskedAddsToList(t *testing.T) {
	for _, c := range []struct {
		name          string
		paths, facets []string
		wantPaths     []string // added to the default list
		wantFacets    []string
	}{
		{"a path", []string{"internal/x/**"}, nil, []string{"internal/x/**"}, nil},
		{"a facet", nil, []string{"code"}, nil, []string{"code"}},
		{"nothing", nil, nil, nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			asActor(t, "")
			repoDir, projDir, _ := newFeatureBackend(t)
			calibrating(t, projDir, 0)
			decided(t, repoDir, "F-0001", domain.StateDone, []string{"code"}, lowPayload())
			if _, err := RecordRiskFeedback(repoDir, projDir, "F-0001", RiskFeedback{Answer: FeedbackShouldHaveAsked, AddPaths: c.paths, AddFacets: c.facets}); err != nil {
				t.Fatal(err)
			}
			cfg, _, err := project.LoadConfig(projDir)
			if err != nil {
				t.Fatal(err)
			}
			if c.wantPaths == nil && cfg.Gates.RiskPaths != nil || c.wantFacets == nil && cfg.Gates.RiskFacets != nil {
				t.Errorf("lists changed with nothing marked: %v / %v", cfg.Gates.RiskPaths, cfg.Gates.RiskFacets)
			}
			for _, p := range c.wantPaths {
				if !slices.Contains(cfg.Gates.RiskHumanPaths(), p) || !slices.Contains(cfg.Gates.RiskHumanPaths(), "go.mod") {
					t.Errorf("paths %v: want %s added to the default list", cfg.Gates.RiskHumanPaths(), p)
				}
			}
			for _, f := range c.wantFacets {
				if !slices.Contains(cfg.Gates.RiskHumanFacets(), f) || !slices.Contains(cfg.Gates.RiskHumanFacets(), "auth") {
					t.Errorf("facets %v: want %s added to the default list", cfg.Gates.RiskHumanFacets(), f)
				}
			}
			p := lastEntityEvent(t, repoDir, "F-0001", riskFeedbackEvent).Payload
			if p["added_paths"] != strings.Join(c.paths, ", ") || p["added_facets"] != strings.Join(c.facets, ", ") {
				t.Errorf("payload %v: want the marks recorded", p)
			}
		})
	}
	asActor(t, "")
	repoDir, projDir, _ := newFeatureBackend(t)
	decided(t, repoDir, "F-0002", domain.StateDone, []string{"code"}, heldPayload())
	if _, err := RecordRiskFeedback(repoDir, projDir, "F-0002", RiskFeedback{Answer: FeedbackNotNeeded, AddPaths: []string{"x/**"}}); !errors.Is(err, ErrFeedbackMismatch) {
		t.Errorf("not-needed with a mark: err = %v, want ErrFeedbackMismatch", err)
	}
	// A facet outside the closed set is refused, and nothing is written.
	calibrating(t, projDir, 0)
	decided(t, repoDir, "F-0003", domain.StateDone, []string{"code"}, lowPayload())
	before, _ := os.ReadFile(filepath.Join(projDir, ".cernerc"))
	if _, err := RecordRiskFeedback(repoDir, projDir, "F-0003", RiskFeedback{Answer: FeedbackShouldHaveAsked, AddPaths: []string{"x/**"}, AddFacets: []string{"billing"}}); !errors.Is(err, domain.ErrUnknownFacet) {
		t.Errorf("--add-facet billing: err = %v, want ErrUnknownFacet", err)
	}
	if after, _ := os.ReadFile(filepath.Join(projDir, ".cernerc")); string(after) != string(before) {
		t.Errorf("a refused facet changed .cernerc:\n%s", after)
	}
	if evs := entityEvents(t, repoDir, "F-0003", riskFeedbackEvent); len(evs) != 0 {
		t.Errorf("a refused facet wrote the answer")
	}
}

// TestAcceptModeHumanAlwaysHolds: a card the owner marked as needing a person
// waits for the merge even with all three conditions holding, and the mark is
// on the complete event.
// F-0518 crit. 15
func TestAcceptModeHumanAlwaysHolds(t *testing.T) {
	asActor(t, "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	riskDelivery(t, repoDir, projDir, store.Feature{ID: "F-0001", Facets: []string{"code"}, AcceptMode: AcceptModeHuman}, "vitória", cleanChange())
	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.To != domain.StateImplemented || out.Risk == nil || out.Risk.Low() || !strings.Contains(strings.Join(out.Risk.Held(), "; "), "needs a person") {
		t.Fatalf("complete = %s, risk %+v; want held by the owner's mark", out.To, out.Risk)
	}
	if got := lastEntityEvent(t, repoDir, "F-0001", "feature.completed").Payload["risk_mode"]; got != AcceptModeHuman {
		t.Errorf("risk_mode on the complete = %q, want human", got)
	}
}

// TestAcceptModeAutoWaivesOnlySensitiveArea: a card the owner marked to pass
// alone passes a sensitive path and facet, but only with a Gauntlet win and
// every criterion tested; a checkout that is not its delivery still holds it.
// F-0518 crit. 15
func TestAcceptModeAutoWaivesOnlySensitiveArea(t *testing.T) {
	for _, c := range []struct {
		name        string
		id          string
		win, tested bool
		low         bool
	}{
		{"win and tested", "F-0001", true, true, true},
		{"tie", "F-0001", false, true, false},
		{"a criterion untested", "F-0001", true, false, false},
		{"neither", "F-0001", false, false, false},
		{"not its delivery", "F-0002", true, true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			repoDir, projDir := t.TempDir(), t.TempDir()
			change := cleanChange()
			change["go.mod"] = "module x\n"
			if !c.tested {
				change["internal/x/x_test.go"] = "package x\n\n// F-0001 crit. 1\n"
			}
			if c.id != "F-0001" {
				change["internal/x/x_test.go"] = "package x\n\n// " + c.id + " crit. 1, 2\n"
			}
			calibrating(t, projDir, 0)
			riskRepo(t, projDir, nil, change)
			outcome := "vitória"
			if !c.win {
				outcome = "empate"
			}
			riskWrite(t, repoDir, filepath.Join("content", "features", c.id, "spec.md"), riskSpec)
			riskWrite(t, repoDir, filepath.Join("content", "features", c.id, "result.md"), riskResult(outcome))
			ra := assessRisk(repoDir, projDir, store.Feature{ID: c.id, Facets: []string{"auth"}, AcceptMode: AcceptModeAuto})
			if ra.Low() != c.low {
				t.Fatalf("Low() = %v, want %v: %+v", ra.Low(), c.low, ra)
			}
			if c.id == "F-0001" && (!ra.Area.Holds || len(ra.Paths) == 0 || len(ra.Facets) == 0) {
				t.Errorf("area %+v, paths %v, facets %v: want the sensitive area waived and still measured", ra.Area, ra.Paths, ra.Facets)
			}
		})
	}
}
