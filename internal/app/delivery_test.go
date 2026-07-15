package app

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/project"
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
