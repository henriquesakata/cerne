package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/project"
	"github.com/henriquesakata/cerne/internal/sourceurl"
	"github.com/henriquesakata/cerne/internal/store"
	"github.com/henriquesakata/cerne/internal/version"
)

// Snapshot is one dated copy of a page in an archive service (F-0503).
type Snapshot struct {
	URL        string
	CapturedAt time.Time
}

// ArchiveClient is the archive service: Save asks for a new copy of a page,
// Closest looks up the copy already there nearest to now. Each gets only the
// page's URL.
type ArchiveClient interface {
	Save(ctx context.Context, url string) (Snapshot, error)
	Closest(ctx context.Context, url string) (Snapshot, error)
}

// ErrNoSnapshot: the archive holds no copy of the page.
var ErrNoSnapshot = errors.New("no archived copy")

// errNoCopyLink: Save answered without the link of a copy.
var errNoCopyLink = errors.New("no copy link")

// httpStatusError is a service answer that is not a success; its text is the
// bare code, safe to record.
type httpStatusError int

func (e httpStatusError) Error() string { return strconv.Itoa(int(e)) }

// WaybackClient is the real ArchiveClient: the Wayback Machine's Save Page Now
// and its availability API (R-0140). The endpoints are fields so a test points
// them at a fake.
type WaybackClient struct {
	SaveURL      string // the page URL is appended: https://web.archive.org/save/<url>
	AvailableURL string // ?url=<url> is added
}

// NewWaybackClient points at the Internet Archive.
func NewWaybackClient() WaybackClient {
	return WaybackClient{SaveURL: "https://web.archive.org/save/", AvailableURL: "https://archive.org/wayback/available"}
}

const (
	waybackSaveTimeout    = 120 * time.Second
	waybackClosestTimeout = 20 * time.Second
	waybackTimestamp      = "20060102150405"
)

// waybackHTTP has no cookie jar and follows no redirect: Save reads the copy's
// link from the redirect itself. The environment proxy is kept.
var waybackHTTP = &http.Client{
	Transport:     http.DefaultTransport.(*http.Transport).Clone(),
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func waybackGet(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cerne/"+version.Version)
	return waybackHTTP.Do(req)
}

// Save asks Save Page Now for a new copy and reads its link from Location or
// Content-Location (/web/<timestamp>/<url>).
func (c WaybackClient) Save(ctx context.Context, target string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, waybackSaveTimeout)
	defer cancel()
	resp, err := waybackGet(ctx, c.SaveURL+target)
	if err != nil {
		return Snapshot{}, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Snapshot{}, httpStatusError(resp.StatusCode)
	}
	for _, h := range []string{"Location", "Content-Location"} {
		if s, ok := copyAt(resp.Request.URL, resp.Header.Get(h)); ok {
			return s, nil
		}
	}
	return Snapshot{}, errNoCopyLink
}

var copyPath = regexp.MustCompile(`^/web/(\d{14})`)

// copyAt resolves a copy link against the request and reads its timestamp;
// false when it is not a /web/<timestamp>/ link.
func copyAt(base *url.URL, link string) (Snapshot, bool) {
	if link == "" {
		return Snapshot{}, false
	}
	u, err := base.Parse(link)
	if err != nil {
		return Snapshot{}, false
	}
	m := copyPath.FindStringSubmatch(u.Path)
	if m == nil {
		return Snapshot{}, false
	}
	at, err := time.Parse(waybackTimestamp, m[1])
	if err != nil {
		return Snapshot{}, false
	}
	return Snapshot{URL: u.String(), CapturedAt: at}, true
}

// Archive statuses recorded on a reference (store.ReferenceArchive.Status).
const (
	archiveArchived  = "archived"
	archivePending   = "pending"
	archiveNotPublic = "not_public"
	archiveSkipped   = "skipped"
)

// ArchiveNet is every network piece an archive run touches (F-0503); a test
// swaps each one.
type ArchiveNet struct {
	Client    ArchiveClient
	LookupIP  func(ctx context.Context, network, host string) ([]net.IP, error)
	Transport http.RoundTripper // the public proof's
	Pause     time.Duration     // between two requests to the archive service
}

// NewArchiveNet is the real network: the Wayback Machine, the system
// resolver, the default transport (environment proxy kept) and a 5 s pause.
func NewArchiveNet() ArchiveNet {
	return ArchiveNet{
		Client:    NewWaybackClient(),
		LookupIP:  net.DefaultResolver.LookupIP,
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
		Pause:     5 * time.Second,
	}
}

// ErrArchiveOff refuses an archive run while references.archive is off.
var ErrArchiveOff = errors.New("references.archive is off")

// archiveWouldSend is a dry run's result for a URL that passed the gate; it
// is never recorded.
const archiveWouldSend = "would_send"

// ArchiveResult is one reference's outcome in an archive run; URL is the
// source's.
type ArchiveResult struct {
	ID  string
	URL string
	store.ReferenceArchive
}

// ArchiveReferences archives the named references, or every current external
// one with no copy yet or a pending one (F-0503). Each URL goes through
// gateArchive; one that passes is sent to Save, then to Closest when Save
// fails, one at a time with n.Pause between two. A dry run stops after the
// gate and records nothing. The network phase runs outside the backend lock;
// the outcomes are recorded in one mutation with a reference.archived event
// each. With references.archive off it refuses before anything.
// ponytail: one commit per run, so a run cut short records nothing (the copies
// already made are found by Closest next time); commit per reference if
// batches of hundreds need it.
func ArchiveReferences(repoDir, projDir string, ids []string, n ArchiveNet, dryRun bool) ([]ArchiveResult, error) {
	on := project.ReferencesArchive(projDir)
	if !on {
		return nil, fmt.Errorf("%w: nothing is sent; a human turns it on in the Settings screen of cerne ui", ErrArchiveOff)
	}
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		return nil, err // no ignore list, no archive
	}
	refs, err := referencesToArchive(repoDir, ids)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	var out []ArchiveResult
	sent := false
	for _, r := range refs {
		res := ArchiveResult{ID: r.ID, URL: r.URL}
		st, why := gateArchive(ctx, r, on, cfg.SourceIgnore(), n)
		switch {
		case st != "":
			res.Status, res.Reason = st, why
		case dryRun:
			res.Status = archiveWouldSend
		default:
			if sent {
				time.Sleep(n.Pause) // ponytail: fixed pause; back off on 429 if the service asks for it
			}
			sent = true
			res.ReferenceArchive = saveOrClosest(ctx, r.URL, n.Client)
		}
		out = append(out, res)
	}
	if dryRun || len(out) == 0 {
		return out, nil
	}
	err = MutateAndSnapshot(repoDir, projDir, func() (string, error) {
		recorded := 0
		for _, res := range out {
			r, err := store.LoadReference(repoDir, res.ID)
			if err != nil {
				return "", err
			}
			if r.State != domain.ReferenceCurrent || r.URL != res.URL {
				continue // retired or repointed meanwhile
			}
			a := res.ReferenceArchive
			r.Archive = &a
			if err := store.SaveReference(repoDir, r); err != nil {
				return "", err
			}
			payload := map[string]string{"status": a.Status}
			for k, v := range map[string]string{"origin": a.Origin, "link": a.URL, "reason": a.Reason} {
				if v != "" {
					payload[k] = v
				}
			}
			if err := store.AppendEvent(repoDir, domain.NewEvent(r.ID, "reference.archived", payload, EventActor())); err != nil {
				return "", err
			}
			recorded++
		}
		return fmt.Sprintf("cerne: archive %d reference(s)", recorded), nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// referencesToArchive is referencesToCheck's targets; with no ids, only the
// ones with no copy yet or a pending one, never a no-archive one.
func referencesToArchive(repoDir string, ids []string) ([]store.Reference, error) {
	refs, err := referencesToCheck(repoDir, ids)
	if err != nil || len(ids) > 0 {
		return refs, err
	}
	return slices.DeleteFunc(refs, func(r store.Reference) bool {
		return r.NoArchive || (r.Archive != nil && r.Archive.Status != archivePending)
	}), nil
}

// saveOrClosest asks for a new copy and, when that fails for any reason,
// takes the closest existing one (crit. 10); both failing is pending.
func saveOrClosest(ctx context.Context, u string, c ArchiveClient) store.ReferenceArchive {
	s, err := c.Save(ctx, u)
	if err == nil {
		return store.ReferenceArchive{URL: s.URL, CapturedAt: s.CapturedAt.UTC(), Origin: "saved", Status: archiveArchived}
	}
	if s, cerr := c.Closest(ctx, u); cerr == nil {
		return store.ReferenceArchive{URL: s.URL, CapturedAt: s.CapturedAt.UTC(), Origin: "existing", Status: archiveArchived}
	}
	return store.ReferenceArchive{Status: archivePending, Reason: "save failed: " + saveFailure(err)}
}

// saveFailure names a Save failure without its raw text, which can carry a
// proxy host or an internal address.
func saveFailure(err error) string {
	var code httpStatusError
	switch {
	case errors.As(err, &code):
		return code.Error()
	case errors.Is(err, errNoCopyLink):
		return "no copy link"
	}
	return transientReason(err)
}

// gateArchive is the one door to the archive service (F-0503): nothing reaches
// Save or Closest unless it returns "". The checks run cheapest and offline
// first — no-archive (crit. 7), the setting (2), internal hosts and ignored
// sources (4), credentials and secrets (5) — so a refusal there opens no
// connection; then the DNS answer and the anonymous GET (6). Otherwise it
// returns the status to record and why.
func gateArchive(ctx context.Context, r store.Reference, on bool, ignore []string, n ArchiveNet) (status, reason string) {
	if r.NoArchive {
		return archiveSkipped, "no-archive"
	}
	if !on {
		return archiveSkipped, "archive off"
	}
	u, err := url.Parse(r.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return archiveSkipped, "not an http(s) URL"
	}
	switch {
	case sourceurl.Internal(r.URL, ignore) || reservedHost(u.Hostname()):
		return archiveSkipped, "internal host"
	case u.User != nil:
		return archiveSkipped, "credentials in URL"
	case sourceurl.HasSecret(r.URL):
		return archiveSkipped, "secret in query"
	}
	if st, why := resolvesPublic(ctx, u.Hostname(), n.LookupIP); st != "" {
		return st, why
	}
	return provePublic(ctx, r.URL, ignore, n.Transport)
}

// reservedSuffixes are names that never resolve on the public Internet.
var reservedSuffixes = []string{".local", ".localhost", ".internal", ".lan", ".home.arpa", ".corp", ".test", ".invalid", ".example"}

// reservedHost is the archive's stricter filter, kept out of lint: a name with
// no dot, a reserved suffix, or an IP literal that is not public.
func reservedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if ip := net.ParseIP(host); ip != nil {
		return !publicIP(ip)
	}
	if !strings.Contains(host, ".") {
		return true
	}
	for _, s := range reservedSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

var sharedAddressSpace = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// publicIP: global unicast, not private, not the carrier-grade NAT range.
func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !sharedAddressSpace.Contains(ip)
}

const dnsTimeout = 10 * time.Second

// resolvesPublic refuses a name any of whose addresses is not public: a
// public-looking intranet name answers 200 from inside the network.
func resolvesPublic(ctx context.Context, host string, lookup func(context.Context, string, string) ([]net.IP, error)) (status, reason string) {
	if net.ParseIP(host) != nil {
		return "", "" // a literal: reservedHost already judged it
	}
	ctx, cancel := context.WithTimeout(ctx, dnsTimeout)
	defer cancel()
	ips, err := lookup(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return archivePending, "dns lookup failed"
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return archiveSkipped, "private address"
		}
	}
	return "", ""
}

const proofTimeout = 15 * time.Second

// loginHosts and loginSegments recognize a login page.
// ponytail: a handful of hosts and path words; a private page that answers 200
// without redirecting to one passes. Upgrade path: per-host rules or a
// login-form sniff of the body.
var (
	loginHosts    = []string{"accounts.google.com", "login.microsoftonline.com", "login.live.com", "id.atlassian.com"}
	loginSegments = []string{"login", "signin", "sign_in", "sign-in", "sso", "oauth", "sessions"}
)

func loginPage(u *url.URL) bool {
	if slices.Contains(loginHosts, strings.ToLower(u.Hostname())) {
		return true
	}
	for _, seg := range strings.Split(strings.ToLower(u.Path), "/") {
		if slices.Contains(loginSegments, seg) {
			return true
		}
	}
	return false
}

// refusedHop stops the proof at a redirect that is not public; its text is
// the reason recorded.
type refusedHop string

func (e refusedHop) Error() string { return string(e) }

// hopRefusal judges each URL the proof lands on, the final one included.
func hopRefusal(u *url.URL, ignore []string) string {
	switch {
	case loginPage(u):
		return "login page"
	case u.Scheme != "http" && u.Scheme != "https", sourceurl.Internal(u.String(), ignore), reservedHost(u.Hostname()):
		return "redirects to an internal host"
	}
	return ""
}

// provePublic is the anonymous GET (crit. 6): its own client, no cookie jar,
// no credential, no Referer, redirects followed and each one judged, the body
// never read. Only a final 200 passes; 429 and 5xx are pending, anything else
// is not public.
func provePublic(ctx context.Context, raw string, ignore []string, rt http.RoundTripper) (status, reason string) {
	client := &http.Client{
		Transport: rt,
		Timeout:   proofTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return refusedHop("too many redirects")
			}
			req.Header.Del("Referer")
			if why := hopRefusal(req.URL, ignore); why != "" {
				return refusedHop(why)
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return archiveSkipped, "not an http(s) URL"
	}
	req.Header.Set("User-Agent", "cerne/"+version.Version)
	resp, err := client.Do(req)
	var hop refusedHop
	if errors.As(err, &hop) {
		return archiveNotPublic, string(hop)
	}
	if err != nil {
		return archivePending, "page unreachable: " + transientReason(err)
	}
	resp.Body.Close()
	if why := hopRefusal(resp.Request.URL, ignore); why != "" {
		return archiveNotPublic, why
	}
	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		return "", ""
	case code == http.StatusTooManyRequests || code >= 500:
		return archivePending, "HTTP " + strconv.Itoa(code)
	default:
		return archiveNotPublic, "HTTP " + strconv.Itoa(code)
	}
}

// Closest asks the availability API for the copy nearest to now.
func (c WaybackClient) Closest(ctx context.Context, target string) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, waybackClosestTimeout)
	defer cancel()
	resp, err := waybackGet(ctx, c.AvailableURL+"?url="+url.QueryEscape(target))
	if err != nil {
		return Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, httpStatusError(resp.StatusCode)
	}
	var body struct {
		ArchivedSnapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Timestamp string `json:"timestamp"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Snapshot{}, err
	}
	cl := body.ArchivedSnapshots.Closest
	at, err := time.Parse(waybackTimestamp, cl.Timestamp)
	if !cl.Available || cl.URL == "" || err != nil {
		return Snapshot{}, ErrNoSnapshot
	}
	return Snapshot{URL: cl.URL, CapturedAt: at}, nil
}
