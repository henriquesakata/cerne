package app

import (
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0503 crit. 6: a final URL whose query says it is a sign-in (wa=wsignin…, a
// return-to or SAML parameter, any case) or whose path has an oauth segment
// (PingFederate) is a login page; an ordinary query is not.
func TestLoginPageQuery(t *testing.T) {
	for _, tc := range []struct {
		u    string
		want bool
	}{
		{"https://x.org/adfs/ls/?wa=wsignin1.0&wtrealm=a", true},
		{"https://x.org/p?startURL=%2Fhome", true},
		{"https://x.org/p?returnUrl=%2Fhome", true},
		{"https://x.org/p?RETURN_TO=%2Fhome", true},
		{"https://x.org/p?redirect_uri=https%3A%2F%2Fa", true},
		{"https://x.org/p?SAMLRequest=abc", true},
		{"https://x.org/p?lang=en&RelayState=abc", true},
		{"https://x.org/p?ec=302", true},
		{"https://x.org/as/authorization.oauth2?client_id=a", true},
		{"https://x.org/p?wa=other", false},
		{"https://x.org/p?lang=en&page=2&q=ec2", false},
		{"https://x.org/docs?reference=1", false},
	} {
		u, err := url.Parse(tc.u)
		if err != nil {
			t.Fatal(err)
		}
		if got := loginPage(u); got != tc.want {
			t.Errorf("loginPage(%s) = %v, want %v", tc.u, got, tc.want)
		}
	}
}

// F-0503 crit. 10: a failed capture list for a reference registered before
// today saves nothing: it is pending, so a later batch retries it; a reference
// registered today still asks for a new copy.
func TestArchiveReferencesCaptureListFailure(t *testing.T) {
	u := "https://example.com/1"
	failing := func(string) ([]Snapshot, error) { return nil, httpStatusError(http.StatusServiceUnavailable) }
	for _, tc := range []struct {
		name    string
		created time.Time
		asked   []string
		status  string
		reason  string
	}{
		{"older", savedAt.Add(-48 * time.Hour), []string{"captures " + u}, "pending", "capture list failed: 503"},
		{"today", savedAt.Add(-2 * time.Hour), []string{"captures " + u, "save " + u}, "archived", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoDir, projDir := archiveBackend(t, 1, true)
			setRef(t, repoDir, projDir, "R-0001", func(r *store.Reference) { r.CreatedAt = tc.created })
			c := &scriptClient{save: saves, captures: failing}
			if _, err := ArchiveReferences(repoDir, projDir, nil, publicNet(c), false); err != nil {
				t.Fatal(err)
			}
			got := loadRef(t, repoDir, "R-0001").Archive
			if got == nil || got.Status != tc.status || got.Reason != tc.reason {
				t.Fatalf("stored %+v, want %s (%s)", got, tc.status, tc.reason)
			}
			if !slices.Equal(c.asked, tc.asked) {
				t.Errorf("asked %v, want %v", c.asked, tc.asked)
			}
		})
	}
}
