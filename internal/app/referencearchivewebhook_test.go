package app

import (
	"context"
	"net/http"
	"testing"
)

// F-0503 crit. 6: a webhook the proof GET could fire is refused before DNS and
// the GET: hook and hooks first labels, pipedream.net, Apps Script macros and
// an n8n /webhook/<id> path; documentation about webhooks passes.
func TestArchiveGateWebhooks(t *testing.T) {
	for _, u := range []string{
		"https://hook.eu1.make.com/abc123", "https://hooks.slack.example.org/x", "https://hooks.zapier.com/hooks/catch/1/2",
		"https://abc.m.pipedream.net/", "https://pipedream.net/x", "https://script.google.com/macros/s/AKfy/exec",
		"https://n8n.acme-corp.com/webhook/8d1c1a2e-0000", "https://n8n.acme-corp.com/a/webhook/xyz/more",
	} {
		if st, why := gateArchive(context.Background(), extRef(u), true, nil, offlineNet(t)); st != archiveSkipped || (why != "webhook" && why != "share link host") {
			t.Errorf("%s: %s (%s), want skipped (webhook)", u, st, why)
		}
	}
	for _, u := range []string{
		"https://docs.github.com/en/webhooks", "https://docs.github.com/en/webhooks/using-webhooks",
		"https://script.google.com/home", "https://n8n.acme-corp.com/webhook", "https://developer.mozilla.org/en-US/docs/Web/API/hooks",
		"https://webhooks.example.org/x",
	} {
		if webhookURL(u) {
			t.Errorf("webhookURL(%s) = true, want false", u)
		}
	}
	// a redirect that lands on a webhook is refused too, with no request to it
	n := offlineNet(t)
	n.LookupIP = publicLookup
	n.Transport = handlerTransport(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/guide" {
			http.Redirect(w, r, "https://hooks.example.org/fire", http.StatusFound)
			return
		}
		t.Errorf("the proof reached %s", r.URL)
	})
	if st, why := gateArchive(context.Background(), extRef("https://docs.publicsite.org/guide"), true, nil, n); st != archiveNotPublic || why != "webhook" {
		t.Errorf("redirect to a webhook: %s (%s), want not_public (webhook)", st, why)
	}
}
