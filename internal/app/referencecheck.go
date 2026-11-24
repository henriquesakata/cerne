package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/version"
)

// referenceTextHash returns the SHA-256 (hex) of what a reader of the page sees
// (F-0501): for HTML, the text outside script/style/noscript/template with
// whitespace collapsed; for any other content type, the raw body.
func referenceTextHash(contentType string, body []byte) string {
	mt, _, _ := mime.ParseMediaType(contentType)
	data := body
	if mt == "text/html" || mt == "application/xhtml+xml" {
		data = []byte(htmlVisibleText(body))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func htmlVisibleText(body []byte) string {
	var sb strings.Builder
	z := html.NewTokenizer(bytes.NewReader(body))
	skip := 0
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.Join(strings.Fields(sb.String()), " ")
		case html.StartTagToken:
			if skippedTag(z) {
				skip++
			}
		case html.EndTagToken:
			if skippedTag(z) && skip > 0 {
				skip--
			}
		case html.TextToken:
			if skip == 0 {
				sb.Write(z.Text())
				sb.WriteByte(' ')
			}
		}
	}
}

func skippedTag(z *html.Tokenizer) bool {
	name, _ := z.TagName()
	switch string(name) {
	case "script", "style", "noscript", "template":
		return true
	}
	return false
}

// FetchResponse is what a Fetcher returns for a URL that answered.
type FetchResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

// Fetcher gets one URL. Only `cerne reference check` ever calls one (F-0501).
type Fetcher func(ctx context.Context, url string) (FetchResponse, error)

// CheckResult is the outcome of checking one reference: Result is ok, changed,
// gone or transient; Detail says why for gone/transient.
type CheckResult struct {
	ID      string
	Result  string
	Status  int
	Detail  string
	Informs []string
}

const checkParallel = 8

// fetched is one URL's raw outcome, gathered outside the backend lock.
type fetched struct {
	url  string
	resp FetchResponse
	err  error
}

// CheckReferences fetches the URL of each current external reference (only ids
// when given) and records the outcome (F-0501). The network phase runs outside
// the backend lock, 8 at a time; the results are then applied in one mutation
// that rereads each reference, so a run is one commit. A transient failure
// (timeout, 5xx, 429, any other error) leaves Reference.Check untouched and is
// only logged on the reference.checked event. An unknown, internal or obsolete
// id refuses the whole call before any fetch.
func CheckReferences(repoDir, projDir string, ids []string, fetch Fetcher) ([]CheckResult, error) {
	refs, err := referencesToCheck(repoDir, ids)
	if err != nil {
		return nil, err
	}
	out := make([]fetched, len(refs))
	sem := make(chan struct{}, checkParallel)
	var wg sync.WaitGroup
	for i, r := range refs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			resp, err := fetch(context.Background(), r.URL)
			out[i] = fetched{url: r.URL, resp: resp, err: err}
		}()
	}
	wg.Wait()

	reached := false // did any reference of this run get an HTTP response?
	for _, o := range out {
		reached = reached || (o.err == nil && o.resp.Status > 0)
	}
	var results []CheckResult
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		results = nil // mutate may rerun
		now := time.Now().UTC()
		for i, was := range refs {
			r, err := store.LoadReference(repoDir, was.ID)
			if err != nil {
				return "", err
			}
			if r.State != domain.ReferenceCurrent || r.URL != out[i].url {
				continue // retired or repointed meanwhile: the fetch no longer applies
			}
			res := classifyCheck(&r, out[i], now, reached)
			res.ID, res.Informs = r.ID, r.Informs
			if res.Result != "transient" {
				r.UpdatedAt = now
				if err := store.SaveReference(repoDir, r); err != nil {
					return "", err
				}
			}
			payload := map[string]string{"result": res.Result, "status": strconv.Itoa(res.Status)}
			if res.Detail != "" {
				payload["detail"] = res.Detail
			}
			if err := store.AppendEvent(repoDir, domain.NewEvent(r.ID, "reference.checked", payload, EventActor())); err != nil {
				return "", err
			}
			results = append(results, res)
		}
		return fmt.Sprintf("cerne: check %d reference(s)", len(results)), nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// referencesToCheck resolves the targets sorted by id: the named ones (each
// must be a current external reference) or every current external one.
func referencesToCheck(repoDir string, ids []string) ([]store.Reference, error) {
	all, err := store.LoadAllReferences(repoDir)
	if err != nil {
		return nil, err
	}
	byID := map[string]store.Reference{}
	for _, r := range all {
		byID[r.ID] = r
	}
	var out []store.Reference
	if len(ids) == 0 {
		for _, r := range all {
			if r.External && r.URL != "" && r.State == domain.ReferenceCurrent {
				out = append(out, r)
			}
		}
	}
	seen := map[string]bool{}
	for _, raw := range ids {
		id := domain.CanonicalID(raw)
		r, ok := byID[id]
		switch {
		case seen[id]:
			continue
		case !ok:
			return nil, fmt.Errorf("reference %s not found in backend: %w", id, store.ErrNotFound)
		case !r.External || r.URL == "":
			return nil, fmt.Errorf("reference %s is not an external source: nothing to check", id)
		case r.State != domain.ReferenceCurrent:
			return nil, fmt.Errorf("reference %s is %s: only current references are checked", id, r.State)
		}
		seen[id] = true
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b store.Reference) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// classifyCheck turns one fetch into a result and, unless it is transient,
// updates r.Check. Only 404, 410 and a domain that does not exist are gone.
func classifyCheck(r *store.Reference, f fetched, now time.Time, reached bool) CheckResult {
	var dns *net.DNSError
	status := f.resp.Status
	switch {
	case f.err != nil && errors.As(f.err, &dns) && dns.IsNotFound && !reached && !networkUp():
		// no source in the run answered: offline or a captive resolver, not a dead domain
		return CheckResult{Result: "transient", Detail: "domain not found (no source reached: network may be down)"}
	case f.err != nil && errors.As(f.err, &dns) && dns.IsNotFound:
		return recordGone(r, 0, "domain not found", now)
	case f.err != nil:
		return CheckResult{Result: "transient", Detail: transientReason(f.err)}
	case status == 404 || status == 410:
		return recordGone(r, status, "HTTP "+strconv.Itoa(status), now)
	case status < 200 || status > 299:
		return CheckResult{Result: "transient", Status: status, Detail: "HTTP " + strconv.Itoa(status)}
	}
	hash := referenceTextHash(f.resp.ContentType, f.resp.Body)
	c := store.ReferenceCheck{CheckedAt: now, HTTPStatus: status, TextHash: hash, BaselineHash: hash, Status: "ok"}
	if r.Check != nil && r.Check.BaselineHash != "" {
		c.BaselineHash = r.Check.BaselineHash
		if hash != c.BaselineHash {
			c.Status = "changed"
		}
	}
	r.Check = &c
	return CheckResult{Result: c.Status, Status: status}
}

// recordGone keeps the previous hashes, so the source coming back unchanged is
// ok again.
func recordGone(r *store.Reference, status int, detail string, now time.Time) CheckResult {
	c := store.ReferenceCheck{CheckedAt: now, HTTPStatus: status, Status: "gone"}
	if r.Check != nil {
		c.TextHash, c.BaselineHash = r.Check.TextHash, r.Check.BaselineHash
	}
	r.Check = &c
	return CheckResult{Result: "gone", Status: status, Detail: detail}
}

const maxCheckBody = 5 << 20

// networkUp reports whether the machine looks online, without any traffic: some
// interface is up, not loopback, and holds a global unicast address. A var so
// tests can pin it.
var networkUp = hasUsableInterface

func hasUsableInterface() bool {
	ifs, err := net.Interfaces()
	if err != nil {
		return false
	}
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.IsGlobalUnicast() {
				return true
			}
		}
	}
	return false
}

// NewHTTPFetcher is the real Fetcher: a plain GET with a generic User-Agent,
// no cookie jar and no credentials, redirects followed, the body cut at 5 MiB
// and each call bound by timeout. The environment proxy is kept.
func NewHTTPFetcher(timeout time.Duration) Fetcher {
	client := &http.Client{
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			req.Header.Del("Referer") // Go adds it on a redirect; send nothing
			return nil
		},
	}
	return func(ctx context.Context, url string) (FetchResponse, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return FetchResponse{}, err
		}
		req.Header.Set("User-Agent", "cerne/"+version.Version)
		resp, err := client.Do(req)
		if err != nil {
			return FetchResponse{}, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxCheckBody))
		if err != nil {
			return FetchResponse{}, err
		}
		return FetchResponse{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"), Body: body}, nil
	}
}

// ErrReferenceNotChanged refuses `reference keep` on a reference whose last
// check is not `changed`: there is no new version to accept.
var ErrReferenceNotChanged = errors.New("reference is not in the changed state")

// KeepReference accepts the current page of a changed reference as its new
// baseline and clears the changed state (F-0501).
func KeepReference(repoDir, projDir, id string) (store.Reference, error) {
	var r store.Reference
	err := MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		var err error
		if r, err = store.LoadReference(repoDir, id); err != nil {
			return "", fmt.Errorf("reference %s: %w", id, err)
		}
		if r.Check == nil || r.Check.Status != "changed" {
			return "", fmt.Errorf("reference %s: %w", id, ErrReferenceNotChanged)
		}
		r.Check.BaselineHash, r.Check.Status = r.Check.TextHash, "ok"
		r.UpdatedAt = time.Now().UTC()
		if err := store.SaveReference(repoDir, r); err != nil {
			return "", err
		}
		ev := domain.NewEvent(id, "reference.kept", map[string]string{"hash": r.Check.TextHash}, EventActor())
		if err := store.AppendEvent(repoDir, ev); err != nil {
			return "", err
		}
		return fmt.Sprintf("cerne: keep %s", id), nil
	})
	return r, err
}

func transientReason(err error) string {
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return "timeout"
	}
	return err.Error()
}
