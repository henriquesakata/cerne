package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

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
