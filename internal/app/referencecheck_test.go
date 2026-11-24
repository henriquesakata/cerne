package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/henriquesakata/cerne/internal/store"
)

// F-0501 crit. 4: the hash is of the visible text, not of the raw HTML.
func TestReferenceTextHash(t *testing.T) {
	const ht = "text/html; charset=utf-8"
	base := referenceTextHash(ht, []byte(`<html><head><style>p{}</style></head><body><p>Hello   world</p><script>var t=1</script></body></html>`))
	if base == "" {
		t.Fatal("empty hash")
	}
	same := []string{
		`<html><head><style>p{color:red}</style></head><body><p>Hello world</p><script>var t=2</script><noscript>x</noscript></body></html>`,
		"<html><body>\n<p>Hello\n\tworld</p>\n</body></html>",
	}
	for _, s := range same {
		if got := referenceTextHash(ht, []byte(s)); got != base {
			t.Errorf("hash of %q differs; want same as base", s)
		}
	}
	if got := referenceTextHash(ht, []byte(`<p>Hello there</p>`)); got == base {
		t.Error("changed visible text must change the hash")
	}
	// non-HTML: raw body, so whitespace counts
	a := referenceTextHash("application/json", []byte(`{"a":1}`))
	b := referenceTextHash("application/json", []byte(`{"a": 1}`))
	if a == "" || a == b {
		t.Errorf("raw-body hash: a=%q b=%q; want distinct non-empty", a, b)
	}
}

func page(text string) FetchResponse {
	return FetchResponse{Status: 200, ContentType: "text/html", Body: []byte("<p>" + text + "</p>")}
}

// refsBackend returns a backend with a feature and n external references
// (R-0001.. informing F-0001).
func refsBackend(t *testing.T, n int) (repoDir, projDir string) {
	t.Helper()
	repoDir, projDir, _ = newFeatureBackend(t)
	if _, err := CreateFeature(repoDir, projDir, "Uses sources", "", "", nil, nil); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= n; i++ {
		u := fmt.Sprintf("https://example.com/%d", i)
		if _, err := CreateExternalReference(repoDir, projDir, "Src "+u, u, "", []string{"F-0001"}, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	}
	return repoDir, projDir
}

func fetchMap(m map[string]FetchResponse) Fetcher {
	return func(_ context.Context, u string) (FetchResponse, error) {
		if r, ok := m[u]; ok {
			return r, nil
		}
		return FetchResponse{Status: 200}, nil
	}
}

func fetchErr(err error) Fetcher {
	return func(context.Context, string) (FetchResponse, error) { return FetchResponse{}, err }
}

func fetchStatus(code int) Fetcher {
	return func(context.Context, string) (FetchResponse, error) { return FetchResponse{Status: code}, nil }
}

func loadRef(t *testing.T, repoDir, id string) store.Reference {
	t.Helper()
	r, err := store.LoadReference(repoDir, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// F-0501 crit. 1: date, status and hash are stored; only the named ids are
// fetched; one run is one backend commit; reference.checked is logged.
func TestCheckReferencesRecordsAndScopes(t *testing.T) {
	repoDir, projDir := refsBackend(t, 2)
	var asked []string
	var mu sync.Mutex
	f := func(_ context.Context, u string) (FetchResponse, error) {
		mu.Lock()
		asked = append(asked, u)
		mu.Unlock()
		return page("hello"), nil
	}
	before := commitCount(t, repoDir)
	res, err := CheckReferences(repoDir, projDir, []string{"R-0002"}, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].ID != "R-0002" || res[0].Result != "ok" || res[0].Status != 200 {
		t.Fatalf("res = %+v", res)
	}
	if len(asked) != 1 || asked[0] != "https://example.com/2" {
		t.Fatalf("fetched %v, want only R-0002's URL", asked)
	}
	c := loadRef(t, repoDir, "R-0002").Check
	if c == nil || c.CheckedAt.IsZero() || c.HTTPStatus != 200 || c.TextHash == "" {
		t.Fatalf("check = %+v", c)
	}
	if loadRef(t, repoDir, "R-0001").Check != nil {
		t.Fatal("R-0001 was not named and must stay unchecked")
	}
	if got := commitCount(t, repoDir) - before; got != 1 {
		t.Fatalf("commits = %d, want 1", got)
	}
	evs, _ := store.ReadEvents(repoDir)
	found := false
	for _, e := range evs {
		if e.EntityID == "R-0002" && e.Type == "reference.checked" {
			found = true
		}
	}
	if !found {
		t.Fatal("no reference.checked event")
	}
	// no ids: every current external reference
	res, err = CheckReferences(repoDir, projDir, nil, f)
	if err != nil || len(res) != 2 || res[0].ID != "R-0001" {
		t.Fatalf("all: %+v, %v", res, err)
	}
}

// F-0501 crit. 2: the first check writes the baseline and raises nothing.
func TestCheckReferencesFirstCheckIsBaseline(t *testing.T) {
	repoDir, projDir := refsBackend(t, 1)
	res, err := CheckReferences(repoDir, projDir, nil, fetchMap(map[string]FetchResponse{"https://example.com/1": page("a")}))
	if err != nil || res[0].Result != "ok" {
		t.Fatalf("%+v, %v", res, err)
	}
	c := loadRef(t, repoDir, "R-0001").Check
	if c.Status != "ok" || c.BaselineHash == "" || c.BaselineHash != c.TextHash {
		t.Fatalf("check = %+v", c)
	}
}

// F-0501 crit. 3: 404, 410 and an unresolvable domain are gone.
func TestCheckReferencesGone(t *testing.T) {
	cases := map[string]Fetcher{
		"404": fetchStatus(404),
		"410": fetchStatus(410),
		"dns": fetchErr(&net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir := refsBackend(t, 1)
			res, err := CheckReferences(repoDir, projDir, nil, f)
			if err != nil || res[0].Result != "gone" || len(res[0].Informs) != 1 {
				t.Fatalf("%+v, %v", res, err)
			}
			if c := loadRef(t, repoDir, "R-0001").Check; c == nil || c.Status != "gone" {
				t.Fatalf("check = %+v", c)
			}
		})
	}
}

// F-0501 crit. 4: a different text is changed and the old baseline stays; the
// source coming back to the baseline is ok again.
func TestCheckReferencesChanged(t *testing.T) {
	repoDir, projDir := refsBackend(t, 1)
	u := "https://example.com/1"
	if _, err := CheckReferences(repoDir, projDir, nil, fetchMap(map[string]FetchResponse{u: page("one")})); err != nil {
		t.Fatal(err)
	}
	base := loadRef(t, repoDir, "R-0001").Check.BaselineHash
	res, err := CheckReferences(repoDir, projDir, nil, fetchMap(map[string]FetchResponse{u: page("two")}))
	if err != nil || res[0].Result != "changed" {
		t.Fatalf("%+v, %v", res, err)
	}
	c := loadRef(t, repoDir, "R-0001").Check
	if c.Status != "changed" || c.BaselineHash != base || c.TextHash == base {
		t.Fatalf("check = %+v, want changed with baseline %s", c, base)
	}
	res, _ = CheckReferences(repoDir, projDir, nil, fetchMap(map[string]FetchResponse{u: page("one")}))
	if res[0].Result != "ok" || loadRef(t, repoDir, "R-0001").Check.Status != "ok" {
		t.Fatalf("back to baseline: %+v", res)
	}
}

// F-0501 crit. 5: a transient failure leaves the previous check untouched and
// is logged on the event.
func TestCheckReferencesTransient(t *testing.T) {
	cases := map[string]Fetcher{
		"timeout": fetchErr(context.DeadlineExceeded),
		"503":     fetchStatus(503),
		"429":     fetchStatus(429),
		"403":     fetchStatus(403),
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			repoDir, projDir := refsBackend(t, 1)
			if _, err := CheckReferences(repoDir, projDir, nil, fetchMap(map[string]FetchResponse{"https://example.com/1": page("a")})); err != nil {
				t.Fatal(err)
			}
			prev := *loadRef(t, repoDir, "R-0001").Check
			res, err := CheckReferences(repoDir, projDir, nil, f)
			if err != nil || res[0].Result != "transient" || res[0].Detail == "" {
				t.Fatalf("%+v, %v", res, err)
			}
			if got := *loadRef(t, repoDir, "R-0001").Check; got != prev {
				t.Fatalf("check changed: %+v -> %+v", prev, got)
			}
			evs, _ := store.ReadEvents(repoDir)
			last := evs[len(evs)-1]
			if last.Type != "reference.checked" || last.Payload["result"] != "transient" {
				t.Fatalf("last event = %+v", last)
			}
		})
	}
}

// F-0501 crit. 1: an unknown or obsolete id refuses before any fetch.
func TestCheckReferencesRefusesBadIDs(t *testing.T) {
	repoDir, projDir := refsBackend(t, 2)
	if _, err := RetireReference(repoDir, projDir, "R-0002", "gone"); err != nil {
		t.Fatal(err)
	}
	called := false
	f := func(context.Context, string) (FetchResponse, error) { called = true; return page("x"), nil }
	for _, id := range []string{"R-0099", "R-0002"} {
		if _, err := CheckReferences(repoDir, projDir, []string{"R-0001", id}, f); err == nil {
			t.Fatalf("%s accepted", id)
		}
	}
	if called {
		t.Fatal("fetcher called despite a bad id")
	}
}
