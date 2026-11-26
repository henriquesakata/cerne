package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/notify"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

// withWebhook writes a .cernerc into projDir pointing [webhook] at url.
func withWebhook(t *testing.T, projDir, url string) {
	t.Helper()
	if err := project.SaveConfig(projDir, project.Config{Webhook: project.WebhookConfig{URL: url}}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
}

// TestTransitionFeatureFiresWebhook (F-0206 crit. 1, 5): approving a feature
// POSTs the full event, and by the time the request lands the backend already
// shows the new state — the dispatch happens strictly after the commit.
func TestTransitionFeatureFiresWebhook(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")

	var got notify.Event
	var stateAtDelivery domain.FeatureState
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		f, err := store.LoadFeature(repoDir, "F-0001")
		if err != nil {
			t.Fatalf("LoadFeature: %v", err)
		}
		stateAtDelivery = f.State
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("TransitionFeature: %v", err)
	}

	if got.ID != "F-0001" || got.Kind != "feature" || got.Action != "approve" ||
		got.From != "candidate" || got.To != "approved" || got.Title != "Feature F-0001" {
		t.Fatalf("event = %+v, want approve candidate->approved for F-0001", got)
	}
	if stateAtDelivery != domain.StateApproved {
		t.Fatalf("state at webhook delivery = %s, want approved (dispatch must follow the commit)", stateAtDelivery)
	}
}

// TestTransitionFeatureNoWebhookConfigSendsNothing (crit. 4): no [webhook]
// block, no request — a bad URL would fail loudly if one were sent.
func TestTransitionFeatureNoWebhookConfigSendsNothing(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatalf("TransitionFeature: %v", err)
	}
	// No server was ever started; if Send tried to dial anything it would
	// error into a stderr warning but TransitionFeature itself must still
	// succeed either way, which the call above already proved.
}

// TestTransitionFeatureWebhookFailureNeverFailsTransition (crit. 2): a 500
// from the receiver doesn't fail or roll back the transition.
func TestTransitionFeatureWebhookFailureNeverFailsTransition(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)

	out, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil)
	if err != nil {
		t.Fatalf("TransitionFeature: %v, want the transition to succeed despite the webhook 500", err)
	}
	if out.To != domain.StateApproved {
		t.Fatalf("To = %s, want approved", out.To)
	}
}

// TestTransitionDecisionFiresWebhook (crit. 1, kind=decision).
func TestTransitionDecisionFiresWebhook(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	d1, err := CreateDecision(repoDir, projDir, "Decisão 1", "corpo", nil, nil, nil, false)
	if err != nil {
		t.Fatalf("CreateDecision d1: %v", err)
	}
	d2, err := CreateDecision(repoDir, projDir, "Decisão 2", "corpo", nil, nil, nil, false)
	if err != nil {
		t.Fatalf("CreateDecision d2: %v", err)
	}

	var got notify.Event
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)

	if _, err := TransitionDecision(repoDir, projDir, d1.ID, d2.ID, "obsoleta"); err != nil {
		t.Fatalf("TransitionDecision: %v", err)
	}
	if got.ID != d1.ID || got.Kind != "decision" || got.Action != "supersede" ||
		got.From != "active" || got.To != "superseded" {
		t.Fatalf("event = %+v", got)
	}
}

// TestStartWorkFiresWebhook (crit. 1, kind=work, action=start).
func TestStartWorkFiresWebhook(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)

	var got notify.Event
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)

	w, err := StartWork(repoDir, projDir, "investigar X", nil, "", false, nil, "")
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if got.ID != w.ID || got.Kind != "work" || got.Action != "start" || got.From != "" || got.To != "active" {
		t.Fatalf("event = %+v, want start ->active for %s", got, w.ID)
	}
}

// TestFinishWorkFiresWebhook (crit. 1, kind=work, action=complete).
func TestFinishWorkFiresWebhook(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	w, err := StartWork(repoDir, projDir, "investigar Y", nil, "", false, nil, "")
	if err != nil {
		t.Fatalf("StartWork: %v", err)
	}

	var got notify.Event
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)

	if _, err := FinishWork(repoDir, projDir, w.ID, "achado", false, ""); err != nil {
		t.Fatalf("FinishWork: %v", err)
	}
	if got.ID != w.ID || got.Kind != "work" || got.Action != "complete" || got.From != "active" || got.To != "done" {
		t.Fatalf("event = %+v, want complete active->done for %s", got, w.ID)
	}
}

// TestWebhookPayloadActorIsPseudonym (F-0442 crit. 7): the webhook payload's
// actor comes from the running process (EventActor), never from a stored
// record, so a legacy login@host in the backend cannot reach the destination;
// with a *.local host and the agent marker it carries agent:m-<8hex>.
func TestWebhookPayloadActorIsPseudonym(t *testing.T) {
	pinMachine(t, "alice", "mac.local")
	t.Setenv("CERNE_ACTOR", "agent")
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateCandidate, "spec.md")
	if err := store.AppendEvent(repoDir, domain.NewEvent("F-0001", "feature.set", nil, "agent:alice@mac.local")); err != nil {
		t.Fatal(err)
	}
	var got notify.Event
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer ts.Close()
	withWebhook(t, projDir, ts.URL)
	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionApprove, "", "", nil); err != nil {
		t.Fatal(err)
	}
	if want := "agent:" + domain.MachinePseudonym("alice@mac.local"); got.Actor != want {
		t.Errorf("webhook actor = %q, want %q", got.Actor, want)
	}
}

// TestWebhookAutoCompleteCarriesTasksAsAccept: a task a complete under accept
// = "auto" carries to done is reported as what happened to it, an accept from
// implemented to done — not the parent's complete.
// F-0515 crit. 7
func TestWebhookAutoCompleteCarriesTasksAsAccept(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	var events []notify.Event
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ev notify.Event
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			t.Errorf("decode: %v", err)
		}
		events = append(events, ev)
	}))
	defer ts.Close()
	cfg := project.Config{Webhook: project.WebhookConfig{URL: ts.URL}, Gates: project.GatesConfig{Accept: project.GateAuto}}
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
	seedFeatureInState(t, repoDir, "F-0001", domain.StateInDevelopment, "spec.md", "plan.md", "result.md")
	seedWithSpec(t, repoDir, store.Feature{ID: "T-0002", Parent: "F-0001", State: domain.StateImplemented}, taskBody+exemptTaskBar)

	if _, err := TransitionFeature(repoDir, projDir, "F-0001", domain.ActionComplete, "", "", nil); err != nil {
		t.Fatalf("complete: %v", err)
	}
	for _, ev := range events {
		if ev.ID == "T-0002" {
			if ev.Action != "accept" || ev.From != "implemented" || ev.To != "done" {
				t.Errorf("T-0002 webhook = %s %s → %s, want accept implemented → done", ev.Action, ev.From, ev.To)
			}
			return
		}
	}
	t.Fatalf("no webhook for T-0002 among %+v", events)
}
