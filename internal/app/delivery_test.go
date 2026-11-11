package app

import (
	"errors"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/store"
)

func deliveryCfg(template string) project.Config {
	return project.Config{Delivery: project.DeliveryConfig{CommitURL: template}}
}

func TestResolveDeliveryFullCommitURL(t *testing.T) {
	d, err := ResolveDelivery(deliveryCfg(""), "https://gitlab.example/repo/-/commit/abc1234", "")
	if err != nil {
		t.Fatalf("ResolveDelivery: %v", err)
	}
	if d.CommitURL != "https://gitlab.example/repo/-/commit/abc1234" {
		t.Fatalf("CommitURL: got %q", d.CommitURL)
	}
}

func TestResolveDeliverySHAWithTemplate(t *testing.T) {
	cfg := deliveryCfg("https://gitlab.example/repo/-/commit/{sha}")
	cases := []struct{ name, sha, want string }{
		{"short", "abc1234", "https://gitlab.example/repo/-/commit/abc1234"},
		{"full", "0123456789abcdef0123456789abcdef01234567", "https://gitlab.example/repo/-/commit/0123456789abcdef0123456789abcdef01234567"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := ResolveDelivery(cfg, tc.sha, "")
			if err != nil {
				t.Fatalf("ResolveDelivery: %v", err)
			}
			if d.CommitURL != tc.want {
				t.Fatalf("CommitURL: got %q, want %q", d.CommitURL, tc.want)
			}
		})
	}
}

func TestResolveDeliverySHAWithoutTemplateFails(t *testing.T) {
	_, err := ResolveDelivery(deliveryCfg(""), "abc1234", "")
	if !errors.Is(err, ErrDeliveryNoTemplate) {
		t.Fatalf("want ErrDeliveryNoTemplate, got %v", err)
	}
	// A template without the {sha} placeholder cannot resolve either.
	_, err = ResolveDelivery(deliveryCfg("https://gitlab.example/repo/-/commit/"), "abc1234", "")
	if !errors.Is(err, ErrDeliveryNoTemplate) {
		t.Fatalf("want ErrDeliveryNoTemplate for template without {sha}, got %v", err)
	}
}

func TestResolveDeliveryInvalidInputFails(t *testing.T) {
	cfg := deliveryCfg("https://gitlab.example/repo/-/commit/{sha}")
	cases := []struct{ name, commit, mr string }{
		{"commit ftp scheme", "ftp://host/x", ""},
		{"commit random string", "not-a-sha!", ""},
		{"commit hex too short", "abc123", ""},
		{"mr not a url", "", "merge-request-7"},
		{"mr bare sha", "", "abc1234"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveDelivery(cfg, tc.commit, tc.mr); !errors.Is(err, ErrDeliveryInvalid) {
				t.Fatalf("want ErrDeliveryInvalid, got %v", err)
			}
		})
	}
}

// TestSetFeatureDeliveryOnImplemented (F-0179 crit. 7): the post-hoc update
// lands the links on a feature already implemented (the real SHA only exists
// after the squash-merge), appends feature.delivered and leaves state alone.
func TestSetFeatureDeliveryOnImplemented(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateImplemented)

	d := Delivery{
		CommitURL: "https://gitlab.example/repo/-/commit/def5678",
		MRURL:     "https://gitlab.example/repo/-/merge_requests/9",
	}
	if err := SetFeatureDelivery(repoDir, projDir, "F-0001", d); err != nil {
		t.Fatalf("SetFeatureDelivery: %v", err)
	}
	got, err := store.LoadFeature(repoDir, "F-0001")
	if err != nil {
		t.Fatalf("LoadFeature: %v", err)
	}
	if got.State != domain.StateImplemented {
		t.Fatalf("state changed to %s, want implemented untouched", got.State)
	}
	if got.DeliveryCommitURL != d.CommitURL || got.DeliveryMRURL != d.MRURL {
		t.Fatalf("delivery = %q / %q, want %q / %q", got.DeliveryCommitURL, got.DeliveryMRURL, d.CommitURL, d.MRURL)
	}
	evs, err := store.ReadEvents(repoDir)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	last := evs[len(evs)-1]
	if last.Type != "feature.delivered" || last.EntityID != "F-0001" ||
		last.Payload["commit_url"] != d.CommitURL || last.Payload["mr_url"] != d.MRURL {
		t.Fatalf("last event = %+v, want feature.delivered with urls", last)
	}
}

// TestSetFeatureDeliveryOnTerminalFails (crit. 8): closed history
// (rejected/superseded) gets no delivery; empty input is refused too.
func TestSetFeatureDeliveryOnTerminalFails(t *testing.T) {
	repoDir, projDir, _ := newFeatureBackend(t)
	seedFeatureInState(t, repoDir, "F-0001", domain.StateRejected)
	seedFeatureInState(t, repoDir, "F-0002", domain.StateSuperseded)

	d := Delivery{MRURL: "https://gitlab.example/repo/-/merge_requests/9"}
	for _, id := range []string{"F-0001", "F-0002"} {
		if err := SetFeatureDelivery(repoDir, projDir, id, d); !errors.Is(err, ErrDeliveryState) {
			t.Fatalf("%s: want ErrDeliveryState, got %v", id, err)
		}
	}
	seedFeatureInState(t, repoDir, "F-0003", domain.StateImplemented)
	if err := SetFeatureDelivery(repoDir, projDir, "F-0003", Delivery{}); !errors.Is(err, ErrDeliveryInvalid) {
		t.Fatalf("empty delivery: want ErrDeliveryInvalid, got %v", err)
	}
}

func TestResolveDeliveryMRAndEmpty(t *testing.T) {
	d, err := ResolveDelivery(deliveryCfg(""), "", "https://gitlab.example/repo/-/merge_requests/7")
	if err != nil {
		t.Fatalf("ResolveDelivery: %v", err)
	}
	if d.MRURL != "https://gitlab.example/repo/-/merge_requests/7" || d.CommitURL != "" {
		t.Fatalf("got %+v", d)
	}
	empty, err := ResolveDelivery(deliveryCfg(""), "", "")
	if err != nil || empty != (Delivery{}) {
		t.Fatalf("empty input must yield zero Delivery, got %+v / %v", empty, err)
	}
}
