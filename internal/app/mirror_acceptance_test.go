package app

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/tracker/trackertest"
)

// Acceptance tests of the mirror core (F-0449, criteria 4 to 9), written from
// the spec and the plan's contracts against trackertest.Fake: kind "fake",
// configured through the settings registry like a human would.

// asHuman clears every agent marker actorEnv reads (N-0058: unset, not "").
func asHuman(t *testing.T) {
	t.Helper()
	for _, k := range []string{"CERNE_ACTOR", "COPILOT_CLI", "CURSOR_AGENT", "CODEX_THREAD_ID"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

type mirrorConf struct {
	off     bool              // leave mirror.enabled unset (off)
	closeOn string            // mirror.close_on; "" = unset (accept)
	labels  map[string]string // tracker name -> its label
}

// mirrorProject writes a .cernerc into projDir, registers the fakes and
// configures one kind "fake" tracker per fake, in order, through the registry.
func mirrorProject(t *testing.T, projDir string, c mirrorConf, fakes ...*trackertest.Fake) {
	t.Helper()
	rc := "project_id = \"p\"\nbackend = \"b\"\ndefault_scope = \"\"\n"
	if err := os.WriteFile(filepath.Join(projDir, ".cernerc"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	trackertest.Register(t, fakes...)
	var recs []any
	for _, f := range fakes {
		r := map[string]any{"name": f.Name, "kind": "fake", "url": "https://fake.test/o/" + f.Name}
		if l := c.labels[f.Name]; l != "" {
			r["label"] = l
		}
		recs = append(recs, r)
	}
	set := func(id string, v any) {
		t.Helper()
		if err := project.SetSettingValue(projDir, id, project.ScopeProject, v); err != nil {
			t.Fatalf("set %s: %v", id, err)
		}
	}
	if len(recs) > 0 {
		set("mirror.trackers", recs)
	}
	if !c.off {
		set("mirror.enabled", true)
	}
	if c.closeOn != "" {
		set("mirror.close_on", c.closeOn)
	}
}

// issueOn opens the issue an earlier opt-in of card left on f, in state s,
// and forgets the call.
func issueOn(t *testing.T, f *trackertest.Fake, card string, s domain.FeatureState) tracker.Ref {
	t.Helper()
	r, err := f.Create(context.Background(), tracker.Item{CardID: card, State: s, Title: "Public " + card, Summary: "Public summary of " + card})
	if err != nil {
		t.Fatal(err)
	}
	f.Calls = nil
	return r
}

// seedMirrored seeds card id in state, with docs, linked to issue r.
func seedMirrored(t *testing.T, repoDir, id string, state domain.FeatureState, r tracker.Ref, docs ...string) *store.MirrorRef {
	t.Helper()
	seedFeatureInState(t, repoDir, id, state, docs...)
	f := reload(t, repoDir, id)
	f.Mirror = &store.MirrorRef{Tracker: r.Tracker, Kind: "fake", ID: r.ID, URL: r.URL, Title: "Public " + id, Summary: "Public summary of " + id}
	if err := store.SaveFeature(repoDir, f); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "mirror "+id); err != nil {
		t.Fatal(err)
	}
	return f.Mirror
}

func refOf(m *store.MirrorRef) tracker.Ref {
	return tracker.Ref{Tracker: m.Tracker, ID: m.ID, URL: m.URL}
}

// lastUpdate is the last Update f received for card; a transition or a sync
// never creates an issue.
func lastUpdate(t *testing.T, f *trackertest.Fake, card string) trackertest.Call {
	t.Helper()
	var got []trackertest.Call
	for _, c := range f.Calls {
		if c.Op == "Create" {
			t.Fatalf("tracker %s got a Create (%+v); only the opt-in creates", f.Name, c.Item)
		}
		if c.Op == "Update" && c.Item.CardID == card {
			got = append(got, c)
		}
	}
	if len(got) == 0 {
		t.Fatalf("tracker %s got no Update for %s; calls = %+v", f.Name, card, f.Calls)
	}
	return got[len(got)-1]
}

// wantUpdate checks the last Update for the card of m: its ref, and an Item
// carrying the card's state and the link's public text, nothing else.
func wantUpdate(t *testing.T, f *trackertest.Fake, card string, m *store.MirrorRef, state domain.FeatureState) trackertest.Call {
	t.Helper()
	c := lastUpdate(t, f, card)
	if c.Ref != refOf(m) {
		t.Fatalf("Update ref = %+v, want %+v", c.Ref, refOf(m))
	}
	got := c.Item
	got.Successor = nil
	if want := (tracker.Item{CardID: card, State: state, Title: m.Title, Summary: m.Summary}); got != want {
		t.Fatalf("Update item = %+v, want %+v", c.Item, want)
	}
	return c
}

func mirrorWarnings(ws []string) []string {
	var out []string
	for _, w := range ws {
		if strings.HasPrefix(w, "cerne: mirror ") {
			out = append(out, w)
		}
	}
	return out
}

// F-0449 crit. 4: a human's opt-in creates the issue on the first tracker by
// default, with only the public title and summary and the card's state, and
// records the link on the card with a feature.mirrored event.
func TestFeatureMirrorCreatesIssue(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
	mirrorProject(t, projDir, mirrorConf{}, pub, second)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateApproved, "spec.md")
	head := backendHead(t, repoDir)

	f, err := MirrorFeature(repoDir, projDir, "F-0001", "", "Public title", "Public summary")
	if err != nil {
		t.Fatalf("MirrorFeature: %v", err)
	}
	want := &store.MirrorRef{Tracker: "pub", Kind: "fake", ID: "1", URL: "https://fake.test/pub/issues/1", Title: "Public title", Summary: "Public summary"}
	if !reflect.DeepEqual(f.Mirror, want) {
		t.Fatalf("returned mirror = %+v, want %+v", f.Mirror, want)
	}
	if got := reload(t, repoDir, "F-0001").Mirror; !reflect.DeepEqual(got, want) {
		t.Fatalf("stored mirror = %+v, want %+v", got, want)
	}
	if len(pub.Calls) != 1 || pub.Calls[0].Op != "Create" ||
		pub.Calls[0].Item != (tracker.Item{CardID: "F-0001", State: domain.StateApproved, Title: "Public title", Summary: "Public summary"}) {
		t.Fatalf("first tracker calls = %+v, want one Create of the public text in state approved", pub.Calls)
	}
	if len(second.Calls) != 0 {
		t.Fatalf("second tracker calls = %+v, want none (default is the first)", second.Calls)
	}
	iss := pub.Issues["1"]
	if iss.Title != "Public title" || iss.Body != "Public summary\n\ncerne F-0001" || !iss.Open || strings.Contains(iss.Title+iss.Body, "Feature F-0001") {
		t.Fatalf("issue = %+v, want only the public text, open", iss)
	}
	if backendHead(t, repoDir) == head {
		t.Fatal("the link was not committed to the backend")
	}
	evs := entityEvents(t, repoDir, "F-0001", "feature.mirrored")
	if len(evs) != 1 || evs[0].Payload["tracker"] != "pub" || evs[0].Payload["url"] != want.URL {
		t.Fatalf("feature.mirrored events = %+v, want one with tracker pub and url %s", evs, want.URL)
	}
}

// F-0449 crit. 4: an agent is refused before anything is read — whatever the
// card, tracker or text — and nothing is sent or written.
func TestFeatureMirrorRefusesAgent(t *testing.T) {
	for _, c := range []struct{ name, actor, copilot string }{
		{"CERNE_ACTOR=agent", "agent", ""},
		{"CERNE_ACTOR=agent:dev", "agent:dev", ""},
		{"COPILOT_CLI=1", "", "1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			asHuman(t)
			repoDir, projDir, _ := newFeatureBackend(t)
			pub := &trackertest.Fake{Name: "pub"}
			mirrorProject(t, projDir, mirrorConf{}, pub)
			seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
			head := backendHead(t, repoDir)
			t.Setenv("CERNE_ACTOR", c.actor)
			t.Setenv("COPILOT_CLI", c.copilot)

			if _, err := MirrorFeature(repoDir, projDir, "F-0001", "", "Public title", "Public summary"); !errors.Is(err, ErrHumanGate) {
				t.Fatalf("MirrorFeature as agent = %v, want ErrHumanGate", err)
			}
			if _, err := MirrorFeature(repoDir, projDir, "F-9999", "nope", "", ""); !errors.Is(err, ErrHumanGate) {
				t.Fatalf("MirrorFeature as agent on a missing card = %v, want ErrHumanGate before any read", err)
			}
			if len(pub.Calls) != 0 {
				t.Fatalf("tracker calls = %+v, want none", pub.Calls)
			}
			if m := reload(t, repoDir, "F-0001").Mirror; m != nil {
				t.Fatalf("card mirror = %+v, want none", m)
			}
			if backendHead(t, repoDir) != head {
				t.Fatal("a refused opt-in committed to the backend")
			}
		})
	}
}

// F-0449 crit. 4: an empty (or blank) public title or summary is refused,
// with nothing sent or written.
func TestFeatureMirrorRefusesEmptyText(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	head := backendHead(t, repoDir)
	for _, c := range []struct{ title, summary string }{{"", "Public summary"}, {"Public title", ""}, {"", ""}, {"  ", "Public summary"}, {"Public title", " \n "}} {
		if _, err := MirrorFeature(repoDir, projDir, "F-0001", "", c.title, c.summary); !errors.Is(err, ErrMirrorText) {
			t.Errorf("MirrorFeature(title %q, summary %q) = %v, want ErrMirrorText", c.title, c.summary, err)
		}
	}
	if len(pub.Calls) != 0 || reload(t, repoDir, "F-0001").Mirror != nil || backendHead(t, repoDir) != head {
		t.Fatalf("a refused opt-in sent %+v or wrote to the backend", pub.Calls)
	}
}

// F-0449 crit. 4: with mirror.enabled off nothing goes to the tracker, even
// for a human with trackers configured.
func TestFeatureMirrorRefusesWhenDisabled(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{off: true}, pub)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	head := backendHead(t, repoDir)
	if _, err := MirrorFeature(repoDir, projDir, "F-0001", "", "Public title", "Public summary"); !errors.Is(err, ErrMirrorOff) {
		t.Fatalf("MirrorFeature with the mirror off = %v, want ErrMirrorOff", err)
	}
	if len(pub.Calls) != 0 || reload(t, repoDir, "F-0001").Mirror != nil || backendHead(t, repoDir) != head {
		t.Fatalf("a refused opt-in sent %+v or wrote to the backend", pub.Calls)
	}
}

// F-0449 crit. 4: a card already linked is not published twice, on the same
// tracker or another.
func TestFeatureMirrorRefusesAlreadyMirrored(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
	mirrorProject(t, projDir, mirrorConf{}, pub, second)
	m := seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, pub, "F-0001", domain.StateCandidate), "spec.md")
	for _, name := range []string{"", "pub", "second"} {
		if _, err := MirrorFeature(repoDir, projDir, "F-0001", name, "Another title", "Another summary"); !errors.Is(err, ErrAlreadyMirrored) {
			t.Errorf("MirrorFeature(--tracker %q) on a linked card = %v, want ErrAlreadyMirrored", name, err)
		}
	}
	if len(pub.Calls)+len(second.Calls) != 0 {
		t.Fatalf("tracker calls = %+v / %+v, want none", pub.Calls, second.Calls)
	}
	if got := reload(t, repoDir, "F-0001").Mirror; !reflect.DeepEqual(got, m) {
		t.Fatalf("card mirror = %+v, want it unchanged %+v", got, m)
	}
}

// F-0449 crit. 8: --tracker picks the second configured tracker, the link
// names it, and the commit trailer cites its issue; a name not configured is
// refused with nothing sent.
func TestMirrorSecondTracker(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
	mirrorProject(t, projDir, mirrorConf{}, pub, second)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md")
	seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate, "spec.md")

	f, err := MirrorFeature(repoDir, projDir, "F-0001", "second", "Public title", "Public summary")
	if err != nil {
		t.Fatalf("MirrorFeature --tracker second: %v", err)
	}
	if f.Mirror == nil || f.Mirror.Tracker != "second" || f.Mirror.URL != "https://fake.test/second/issues/1" {
		t.Fatalf("mirror = %+v, want the second tracker's issue", f.Mirror)
	}
	if len(pub.Calls) != 0 || len(second.Calls) != 1 || second.Calls[0].Op != "Create" || second.Calls[0].Item.State != domain.StateInDevelopment {
		t.Fatalf("calls = %+v / %+v, want one Create on second only", pub.Calls, second.Calls)
	}
	if got := MirrorTrailers(repoDir, projDir, []string{"F-0001"}); !slices.Equal(got, []string{"Refs: https://fake.test/second/issues/1"}) {
		t.Fatalf("MirrorTrailers = %q, want the second tracker's issue", got)
	}

	if _, err := MirrorFeature(repoDir, projDir, "F-0002", "nope", "Public title", "Public summary"); !errors.Is(err, ErrUnknownTracker) {
		t.Fatalf("MirrorFeature --tracker nope = %v, want ErrUnknownTracker", err)
	}
	if len(pub.Calls) != 0 || len(second.Calls) != 1 || reload(t, repoDir, "F-0002").Mirror != nil {
		t.Fatal("an unknown tracker sent or wrote something")
	}
}

// F-0449 crit. 9: internal/app/mirror.go depends only on the contract — stdlib
// plus domain, project, store and tracker — and names nothing GitHub-specific.
// The label pattern is cerne: followed by a non-space, so the fixed warning
// line "cerne: mirror …" does not count.
func TestMirrorCoreImportsOnlyContract(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "mirror.go", nil, 0)
	if err != nil {
		t.Fatalf("parse mirror.go: %v", err)
	}
	const mod = "github.com/henriquesakata/cerne/internal/"
	allowed := map[string]bool{mod + "domain": true, mod + "project": true, mod + "store": true, mod + "tracker": true}
	var hasContract bool
	for _, imp := range file.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		hasContract = hasContract || p == mod+"tracker"
		if first, _, _ := strings.Cut(p, "/"); strings.Contains(first, ".") && !allowed[p] {
			t.Errorf("mirror.go imports %s; only the stdlib and %v are allowed", p, slices.Sorted(maps.Keys(allowed)))
		}
	}
	if !hasContract {
		t.Error("mirror.go does not import internal/tracker: the core must go through the IssueTracker contract")
	}
	banned := regexp.MustCompile(`(?i)github|\bgh\b|state_reason|not_planned|cerne:\S`)
	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.Ident:
			if banned.MatchString(x.Name) {
				t.Errorf("mirror.go names identifier %s", x.Name)
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING && banned.MatchString(x.Value) {
				t.Errorf("mirror.go holds literal %s", x.Value)
			}
		}
		return true
	})
}

// F-0449 crit. 5: every transition of a mirrored card hands the tracker the
// card's new state with the link's public text — one subtest per state, the
// task an accept carries along included — and a supersede names the
// successor's issue when the successor is mirrored.
func TestMirrorStateTable(t *testing.T) {
	for _, c := range []struct {
		from   domain.FeatureState
		docs   []string
		action domain.Action
		reason string
		to     domain.FeatureState
	}{
		{domain.StateApproved, []string{"spec.md"}, domain.ActionRevert, "back to discovery", domain.StateCandidate},
		{domain.StateCandidate, []string{"spec.md"}, domain.ActionApprove, "", domain.StateApproved},
		{domain.StateApproved, []string{"spec.md", "plan.md"}, domain.ActionStart, "", domain.StateInDevelopment},
		{domain.StateInDevelopment, []string{"spec.md", "plan.md", "result.md"}, domain.ActionComplete, "", domain.StateImplemented},
		{domain.StateImplemented, nil, domain.ActionAccept, "", domain.StateDone},
		// B-0076 crit. 7: a card with an issue of its own still closes it on reject.
		{domain.StateCandidate, nil, domain.ActionReject, "not now", domain.StateRejected},
	} {
		t.Run(string(c.to), func(t *testing.T) {
			asHuman(t)
			repoDir, projDir, _ := newFeatureBackend(t)
			pub := &trackertest.Fake{Name: "pub"}
			mirrorProject(t, projDir, mirrorConf{}, pub)
			m := seedMirrored(t, repoDir, "F-0001", c.from, issueOn(t, pub, "F-0001", c.from), c.docs...)
			out, err := TransitionFeature(repoDir, projDir, "F-0001", c.action, "", c.reason, nil)
			if err != nil {
				t.Fatalf("%s: %v", c.action, err)
			}
			if w := mirrorWarnings(out.Warnings); len(w) != 0 {
				t.Fatalf("warnings = %q, want none", w)
			}
			if call := wantUpdate(t, pub, "F-0001", m, c.to); call.Item.Successor != nil {
				t.Fatalf("Successor = %+v, want none outside a supersede", call.Item.Successor)
			}
		})
	}

	t.Run("superseded by a mirrored successor", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		m := seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, pub, "F-0001", domain.StateCandidate))
		succ := seedMirrored(t, repoDir, "F-0002", domain.StateCandidate, issueOn(t, pub, "F-0002", domain.StateCandidate))
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionSupersede, "F-0002", "merged into F-0002", nil); err != nil {
			t.Fatalf("supersede: %v", err)
		}
		call := wantUpdate(t, pub, "F-0001", m, domain.StateSuperseded)
		if call.Item.Successor == nil || *call.Item.Successor != refOf(succ) {
			t.Fatalf("Successor = %+v, want %+v", call.Item.Successor, refOf(succ))
		}
	})
	t.Run("superseded by a successor not mirrored", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		m := seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, pub, "F-0001", domain.StateCandidate))
		seedFeatureInState(t, repoDir, "F-0002", domain.StateCandidate)
		if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionSupersede, "F-0002", "merged into F-0002", nil); err != nil {
			t.Fatalf("supersede: %v", err)
		}
		if call := wantUpdate(t, pub, "F-0001", m, domain.StateSuperseded); call.Item.Successor != nil {
			t.Fatalf("Successor = %+v, want none: F-0002 is not mirrored", call.Item.Successor)
		}
	})
	t.Run("done for a task the accept carries along", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		epic := seedMirrored(t, repoDir, "E-0001", domain.StateImplemented, issueOn(t, pub, "E-0001", domain.StateImplemented))
		r := issueOn(t, pub, "T-0002", domain.StateImplemented)
		task := &store.MirrorRef{Tracker: r.Tracker, Kind: "fake", ID: r.ID, URL: r.URL, Title: "Public T-0002", Summary: "Public summary of T-0002"}
		seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "E-0001", State: domain.StateImplemented, Mirror: task}, taskBody+exemptTaskBar)
		if _, err := TransitionFeature(repoDir, projDir, "E-0001", domain.ActionAccept, "", "", nil); err != nil {
			t.Fatalf("accept E-0001: %v", err)
		}
		if got := reload(t, repoDir, "T-0002").State; got != domain.StateDone {
			t.Fatalf("T-0002 state = %s, want done (carried along)", got)
		}
		wantUpdate(t, pub, "E-0001", epic, domain.StateDone)
		wantUpdate(t, pub, "T-0002", task, domain.StateDone)
	})
}

// F-0449 crit. 5: a failing tracker, or one past the deadline, never blocks
// the transition: the state is written and the outcome carries the warning
// line the CLI prints.
func TestMirrorFailureDoesNotBlockTransition(t *testing.T) {
	setup := func(t *testing.T) (repoDir, projDir string, pub *trackertest.Fake) {
		asHuman(t)
		repoDir, projDir, _ = newFeatureBackend(t)
		pub = &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, pub, "F-0001", domain.StateCandidate), "spec.md")
		return repoDir, projDir, pub
	}
	t.Run("tracker error", func(t *testing.T) {
		repoDir, projDir, pub := setup(t)
		pub.Err = errors.New("tracker down")
		out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
		if err != nil {
			t.Fatalf("approve with the tracker down: %v, want success", err)
		}
		if out.To != domain.StateApproved || reload(t, repoDir, "F-0001").State != domain.StateApproved {
			t.Fatalf("state = %s / %s, want approved", out.To, reload(t, repoDir, "F-0001").State)
		}
		want := "cerne: mirror F-0001 -> pub: tracker down; run 'cerne mirror sync'"
		if got := mirrorWarnings(out.Warnings); !slices.Equal(got, []string{want}) {
			t.Fatalf("mirror warnings = %q, want exactly [%q]", got, want)
		}
	})
	t.Run("tracker past the deadline", func(t *testing.T) {
		repoDir, projDir, pub := setup(t)
		old := mirrorTimeout
		mirrorTimeout = 50 * time.Millisecond
		t.Cleanup(func() { mirrorTimeout = old })
		pub.Delay = time.Minute
		start := time.Now()
		out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
		if took := time.Since(start); took > 30*time.Second {
			t.Fatalf("approve took %s: the transition waited for the tracker", took)
		}
		if err != nil || reload(t, repoDir, "F-0001").State != domain.StateApproved {
			t.Fatalf("approve past the deadline: %v, state %s; want success, approved", err, reload(t, repoDir, "F-0001").State)
		}
		got := mirrorWarnings(out.Warnings)
		if len(got) != 1 || !strings.HasPrefix(got[0], "cerne: mirror F-0001 -> pub: ") || !strings.HasSuffix(got[0], "; run 'cerne mirror sync'") {
			t.Fatalf("mirror warnings = %q, want one line cerne: mirror F-0001 -> pub: <reason>; run 'cerne mirror sync'", got)
		}
	})
}

// F-0449 crit. 4 and 5: with the mirror off, a mirrored card's transition
// sends nothing; with it on, a card that never opted in sends nothing.
func TestMirrorOffSendsNothing(t *testing.T) {
	t.Run("mirror off", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{off: true}, pub)
		seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, issueOn(t, pub, "F-0001", domain.StateCandidate), "spec.md")
		out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		if len(pub.Calls) != 0 || len(mirrorWarnings(out.Warnings)) != 0 {
			t.Fatalf("calls = %+v, warnings = %q; want nothing with the mirror off", pub.Calls, out.Warnings)
		}
	})
	t.Run("card without opt-in", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
		out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		if len(pub.Calls) != 0 || len(mirrorWarnings(out.Warnings)) != 0 {
			t.Fatalf("calls = %+v, warnings = %q; want nothing for a card that never opted in", pub.Calls, out.Warnings)
		}
	})
}

// F-0449 crit. 7: with mirror.close_on = "merge" the core passes NoReopen, on
// the transition and on the sync, so an issue the merge closed stays closed;
// the default (accept) does not.
func TestCloseOnMergeNoReopen(t *testing.T) {
	for _, c := range []struct {
		closeOn  string
		noReopen bool
	}{{"merge", true}, {"", false}} {
		t.Run("close_on="+c.closeOn, func(t *testing.T) {
			asHuman(t)
			repoDir, projDir, _ := newFeatureBackend(t)
			pub := &trackertest.Fake{Name: "pub"}
			mirrorProject(t, projDir, mirrorConf{closeOn: c.closeOn}, pub)
			r := issueOn(t, pub, "F-0001", domain.StateInDevelopment)
			pub.Issues[r.ID].Open = false // the merge's Closes: closed it
			seedMirrored(t, repoDir, "F-0001", domain.StateInDevelopment, r, "spec.md", "plan.md", "result.md")
			if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if got := lastUpdate(t, pub, "F-0001").Item.NoReopen; got != c.noReopen {
				t.Fatalf("transition NoReopen = %v, want %v", got, c.noReopen)
			}
			pub.Calls = nil
			if _, err := SyncMirror(repoDir, projDir); err != nil {
				t.Fatalf("SyncMirror: %v", err)
			}
			if got := lastUpdate(t, pub, "F-0001").Item.NoReopen; got != c.noReopen {
				t.Fatalf("sync NoReopen = %v, want %v", got, c.noReopen)
			}
			if c.noReopen && pub.Issues[r.ID].Open {
				t.Fatal("the issue the merge closed was reopened")
			}
		})
	}
}

func ideaByURL(t *testing.T, repoDir, url string) (store.Idea, bool) {
	t.Helper()
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		t.Fatalf("LoadAllIdeas: %v", err)
	}
	for _, i := range ideas {
		if i.Mirror != nil && i.Mirror.URL == url {
			return i, true
		}
	}
	return store.Idea{}, false
}

func ideaCount(t *testing.T, repoDir string) int {
	t.Helper()
	ideas, err := store.LoadAllIdeas(repoDir)
	if err != nil {
		t.Fatalf("LoadAllIdeas: %v", err)
	}
	return len(ideas)
}

// F-0449 crit. 6: the sync turns each open issue no card or idea is linked to
// into an active idea with its title, body and the link, on every configured
// tracker; an issue already linked to a card is not imported.
func TestMirrorSyncImportsIdeas(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
	mirrorProject(t, projDir, mirrorConf{}, pub, second)
	linked := issueOn(t, pub, "F-0001", domain.StateCandidate)
	seedMirrored(t, repoDir, "F-0001", domain.StateCandidate, linked)
	outside := []struct {
		f           *trackertest.Fake
		ref         tracker.Ref
		title, body string
	}{
		{pub, pub.Open("Crash on start", "Steps: run it twice", ""), "Crash on start", "Steps: run it twice"},
		{pub, pub.Open("Docs typo", "", ""), "Docs typo", ""},
		{second, second.Open("Please add CSV export", "We need it", ""), "Please add CSV export", "We need it"},
	}

	report, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	if len(report.Ideas) != len(outside) {
		t.Fatalf("report ideas = %q, want %d", report.Ideas, len(outside))
	}
	for _, o := range outside {
		i, ok := ideaByURL(t, repoDir, o.ref.URL)
		if !ok {
			t.Fatalf("no idea linked to %s", o.ref.URL)
		}
		want := &store.MirrorRef{Tracker: o.f.Name, Kind: "fake", ID: o.ref.ID, URL: o.ref.URL}
		if i.Title != o.title || i.Body != o.body || i.State != domain.IdeaActive || !reflect.DeepEqual(i.Mirror, want) {
			t.Fatalf("idea = %+v (mirror %+v), want title %q, body %q, active, mirror %+v", i, i.Mirror, o.title, o.body, want)
		}
	}
	if _, ok := ideaByURL(t, repoDir, linked.URL); ok {
		t.Fatal("the issue linked to F-0001 was imported as an idea")
	}
	if n := ideaCount(t, repoDir); n != len(outside) {
		t.Fatalf("ideas = %d, want %d", n, len(outside))
	}
}

// F-0469 crit. 1 and 2: a tracker list that reaches the 100-issue cap earns a
// note (never a warning); 99 issues, or a failed list, earn none.
func TestSyncMirrorNotesListCap(t *testing.T) {
	cases := []struct {
		name     string
		n        int
		label    string
		listFail bool
		want     []string // substrings of the single cap note; nil = no note
	}{
		{"at the cap", 100, "", false, []string{"pub", "100", "not imported", "label"}},
		{"under the cap", 99, "", false, nil},
		{"with label", 100, "bug", false, []string{"pub", `"bug"`, "not imported", "They are imported once fewer than 100 open issues carry it."}},
		{"list fails", 100, "", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			asHuman(t)
			repoDir, projDir, _ := newFeatureBackend(t)
			pub := &trackertest.Fake{Name: "pub"}
			mirrorProject(t, projDir, mirrorConf{labels: map[string]string{"pub": c.label}}, pub)
			for i := 0; i < c.n; i++ {
				pub.Open("Issue "+strconv.Itoa(i), "", c.label)
			}
			if c.listFail {
				pub.Err = errors.New("boom")
			}
			rep, err := SyncMirror(repoDir, projDir)
			if err != nil {
				t.Fatalf("SyncMirror: %v", err)
			}
			var notes []string
			for _, n := range rep.Notes {
				if strings.Contains(n, "not imported") {
					notes = append(notes, n)
				}
			}
			for _, w := range rep.Warnings {
				if strings.Contains(w, "not imported") {
					t.Fatalf("cap note leaked into warnings: %q", rep.Warnings)
				}
			}
			if c.want == nil {
				if len(notes) != 0 {
					t.Fatalf("notes = %q, want no cap note", notes)
				}
				return
			}
			if len(notes) != 1 {
				t.Fatalf("cap notes = %q, want exactly one", notes)
			}
			for _, w := range c.want {
				if !strings.Contains(notes[0], w) {
					t.Errorf("note %q lacks %q", notes[0], w)
				}
			}
		})
	}
}

// F-0449 crit. 6: a second sync imports nothing and commits nothing — not even
// an issue whose idea a human dropped.
func TestMirrorSyncIdempotent(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	r := pub.Open("Crash on start", "Steps", "")
	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatalf("first SyncMirror: %v", err)
	}
	head := backendHead(t, repoDir)
	report, err := SyncMirror(repoDir, projDir)
	if err != nil {
		t.Fatalf("second SyncMirror: %v", err)
	}
	if len(report.Ideas) != 0 || ideaCount(t, repoDir) != 1 || backendHead(t, repoDir) != head {
		t.Fatalf("second sync: report ideas %q, %d ideas, head moved %v; want nothing new", report.Ideas, ideaCount(t, repoDir), backendHead(t, repoDir) != head)
	}

	i, _ := ideaByURL(t, repoDir, r.URL)
	if _, err := DropIdea(repoDir, projDir, i.ID, "spam"); err != nil {
		t.Fatalf("DropIdea: %v", err)
	}
	head = backendHead(t, repoDir)
	if report, err := SyncMirror(repoDir, projDir); err != nil || len(report.Ideas) != 0 || ideaCount(t, repoDir) != 1 || backendHead(t, repoDir) != head {
		t.Fatalf("sync after a drop: %v, report ideas %q, %d ideas; want the dropped issue left alone", err, report.Ideas, ideaCount(t, repoDir))
	}
}

// F-0449 crit. 6: a tracker with a label imports only the open issues that
// carry it; a tracker without one imports every open issue.
func TestMirrorSyncLabelFilter(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
	mirrorProject(t, projDir, mirrorConf{labels: map[string]string{"pub": "public"}}, pub, second)
	labeled := pub.Open("Labeled", "b", "public")
	pub.Open("Unlabeled", "b", "")
	pub.Open("Other label", "b", "bug")
	anyIssue := second.Open("Anything", "b", "")
	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	for _, r := range []tracker.Ref{labeled, anyIssue} {
		if _, ok := ideaByURL(t, repoDir, r.URL); !ok {
			t.Errorf("issue %s was not imported", r.URL)
		}
	}
	if n := ideaCount(t, repoDir); n != 2 {
		t.Fatalf("ideas = %d, want 2 (pub's label filters the other two)", n)
	}
}

// F-0449 crit. 6: the sync reapplies each mirrored card's state — how a failed
// transition (crit. 5) is recovered — warns about a card whose tracker is no
// longer configured, and a failing tracker is a warning, not a stop.
func TestMirrorSyncReappliesState(t *testing.T) {
	t.Run("reapply", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub := &trackertest.Fake{Name: "pub"}
		mirrorProject(t, projDir, mirrorConf{}, pub)
		dev := seedMirrored(t, repoDir, "F-0001", domain.StateInDevelopment, issueOn(t, pub, "F-0001", domain.StateCandidate))
		done := seedMirrored(t, repoDir, "F-0002", domain.StateDone, issueOn(t, pub, "F-0002", domain.StateImplemented))
		seedMirrored(t, repoDir, "F-0003", domain.StateApproved, tracker.Ref{Tracker: "gone", ID: "7", URL: "https://fake.test/gone/issues/7"})
		seedFeatureInState(t, repoDir, "F-0004", domain.StateApproved)

		report, err := SyncMirror(repoDir, projDir)
		if err != nil {
			t.Fatalf("SyncMirror: %v", err)
		}
		wantUpdate(t, pub, "F-0001", dev, domain.StateInDevelopment)
		wantUpdate(t, pub, "F-0002", done, domain.StateDone)
		if iss := pub.Issues[done.ID]; iss.Open {
			t.Fatal("the done card's issue is still open after the sync")
		}
		for _, c := range pub.Calls {
			if c.Item.CardID == "F-0004" {
				t.Fatalf("a card that never opted in reached the tracker: %+v", c)
			}
		}
		applied := strings.Join(report.Applied, "\n")
		if len(report.Applied) != 2 || !strings.Contains(applied, "F-0001") || !strings.Contains(applied, "F-0002") {
			t.Fatalf("report applied = %q, want F-0001 and F-0002", report.Applied)
		}
		warned := strings.Join(report.Warnings, "\n")
		if !strings.Contains(warned, "F-0003") || !strings.Contains(warned, "tracker gone not configured") {
			t.Fatalf("report warnings = %q, want F-0003's tracker gone not configured", report.Warnings)
		}
	})
	t.Run("a failing tracker warns and the others go on", func(t *testing.T) {
		asHuman(t)
		repoDir, projDir, _ := newFeatureBackend(t)
		pub, second := &trackertest.Fake{Name: "pub"}, &trackertest.Fake{Name: "second"}
		mirrorProject(t, projDir, mirrorConf{}, pub, second)
		r := second.Open("Please add CSV export", "We need it", "")
		pub.Err = errors.New("rate limited")
		report, err := SyncMirror(repoDir, projDir)
		if err != nil {
			t.Fatalf("SyncMirror with one tracker failing: %v, want warnings instead", err)
		}
		if !strings.Contains(strings.Join(report.Warnings, "\n"), "pub") {
			t.Fatalf("report warnings = %q, want one naming pub", report.Warnings)
		}
		if _, ok := ideaByURL(t, repoDir, r.URL); !ok {
			t.Fatal("the working tracker's issue was not imported")
		}
	})
}

// F-0449 crit. 6: promoting an imported idea hands its link to the card, the
// issue is no longer an idea candidate, and the card's accept closes it as
// done without rewriting the outside text.
func TestPromotedIdeaCarriesMirror(t *testing.T) {
	asHuman(t)
	repoDir, projDir, _ := newFeatureBackend(t)
	pub := &trackertest.Fake{Name: "pub"}
	mirrorProject(t, projDir, mirrorConf{}, pub)
	r := pub.Open("Crash on start", "Steps: run it twice", "")
	if _, err := SyncMirror(repoDir, projDir); err != nil {
		t.Fatalf("SyncMirror: %v", err)
	}
	idea, ok := ideaByURL(t, repoDir, r.URL)
	if !ok {
		t.Fatal("the outside issue was not imported")
	}
	_, f, err := PromoteIdea(repoDir, projDir, idea.ID, "", "", false)
	if err != nil {
		t.Fatalf("PromoteIdea: %v", err)
	}
	if !reflect.DeepEqual(f.Mirror, idea.Mirror) || !reflect.DeepEqual(reload(t, repoDir, f.ID).Mirror, idea.Mirror) {
		t.Fatalf("card mirror = %+v (stored %+v), want the idea's %+v", f.Mirror, reload(t, repoDir, f.ID).Mirror, idea.Mirror)
	}
	if report, err := SyncMirror(repoDir, projDir); err != nil || len(report.Ideas) != 0 || ideaCount(t, repoDir) != 1 {
		t.Fatalf("sync after the promote: %v, report ideas %q; want no new idea", err, report.Ideas)
	}

	card := reload(t, repoDir, f.ID)
	card.State = domain.StateImplemented
	if err := store.SaveFeature(repoDir, card); err != nil {
		t.Fatal(err)
	}
	if _, err := gitback.CommitAndPush(repoDir, "seed implemented "+f.ID); err != nil {
		t.Fatal(err)
	}
	pub.Calls = nil
	if _, err := TransitionFeature(repoDir, projDir, f.ID, domain.ActionAccept, "", "", nil); err != nil {
		t.Fatalf("accept %s: %v", f.ID, err)
	}
	call := lastUpdate(t, pub, f.ID)
	if call.Ref != r || call.Item.State != domain.StateDone || call.Item.Title != "" || call.Item.Summary != "" {
		t.Fatalf("Update = %+v, want the outside issue, state done, no public text", call)
	}
	if iss := pub.Issues[r.ID]; iss.Open || iss.Title != "Crash on start" || iss.Body != "Steps: run it twice" {
		t.Fatalf("issue = %+v, want closed with its outside text", iss)
	}
}
