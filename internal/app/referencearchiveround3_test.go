package app

import (
	"context"
	"net"
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

// F-0503 crit. 6: the chat-share hosts match exactly, so the public docs on a
// subdomain (learn.chatgpt.com) pass the share check; the webhook, tunnel,
// editor and storage hosts match with their subdomains.
func TestArchiveGateShareLinkRound3(t *testing.T) {
	for _, h := range []string{"chatgpt.com", "chat.openai.com", "claude.ai"} {
		if st, why := gateArchive(context.Background(), extRef("https://"+h+"/share/x"), true, nil, offlineNet(t)); st != archiveSkipped || why != "share link host" {
			t.Errorf("%s: %s (%s), want skipped (share link host)", h, st, why)
		}
	}
	for _, h := range []string{"learn.chatgpt.com", "www.chatgpt.com", "help.openai.com", "docs.claude.ai", "developers.openai.com"} {
		if shareLinkHost(h) {
			t.Errorf("shareLinkHost(%s) = true, want false", h)
		}
	}
	for _, h := range []string{"excalidraw.com", "mega.nz", "loca.lt", "app.github.dev", "githubpreview.dev", "webhook.office.com", "hooks.zapier.com", "maker.ifttt.com"} {
		for _, host := range []string{h, "abc123." + h} {
			if st, why := gateArchive(context.Background(), extRef("https://"+host+"/x"), true, nil, offlineNet(t)); st != archiveSkipped || why != "share link host" {
				t.Errorf("%s: %s (%s), want skipped (share link host)", host, st, why)
			}
		}
	}
}

// F-0503 crit. 4: an address in 64:ff9b::/96 (NAT64) or 2002::/16 (6to4) is
// judged by the IPv4 address it embeds, as a literal and as a DNS answer.
func TestArchiveGateEmbeddedIPv4(t *testing.T) {
	for _, ip := range []string{"64:ff9b::a00:1", "64:ff9b::7f00:1", "64:ff9b::c000:201", "2002:a00:1::1", "2002:7f00:1::", "2002:c000:201::5", "2002:6440:1::"} {
		if publicIP(net.ParseIP(ip)) {
			t.Errorf("publicIP(%s) = true, want false (embedded private IPv4)", ip)
		}
		if st, why := gateArchive(context.Background(), extRef("http://["+ip+"]/x"), true, nil, offlineNet(t)); st != archiveSkipped || why != "internal host" {
			t.Errorf("literal %s: %s (%s), want skipped (internal host)", ip, st, why)
		}
	}
	for _, ip := range []string{"64:ff9b::5db8:d70e", "2002:5db8:d70e::1"} {
		if !publicIP(net.ParseIP(ip)) {
			t.Errorf("publicIP(%s) = false, want true (embedded public IPv4)", ip)
		}
	}
}

// F-0503 crit. 9: a real run reports each reference as soon as it is processed,
// before the single commit, so a run cut short still shows which URLs left the
// machine; a dry run reports nothing along the way.
func TestArchiveReferencesStreamsEachResult(t *testing.T) {
	repoDir, projDir := archiveBackend(t, 3, true)
	before := commitCount(t, repoDir)
	var seen []string
	n := publicNet(&scriptClient{save: saves, captures: noCaptures})
	n.Progress = func(r ArchiveResult) {
		if got := commitCount(t, repoDir); got != before {
			t.Errorf("%s reported after the commit (%d commits, want %d)", r.ID, got, before)
		}
		seen = append(seen, r.ID+" "+r.Status)
	}
	if _, err := ArchiveReferences(repoDir, projDir, nil, n, false); err != nil {
		t.Fatal(err)
	}
	if want := []string{"R-0001 archived", "R-0002 archived", "R-0003 archived"}; !slices.Equal(seen, want) {
		t.Errorf("progress = %v, want %v", seen, want)
	}
	seen = nil
	if _, err := ArchiveReferences(repoDir, projDir, []string{"R-0001"}, n, true); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Errorf("a dry run reported %v along the way", seen)
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
