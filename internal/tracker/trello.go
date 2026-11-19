package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/version"
)

// trello mirrors cards as cards on one Trello board over its REST API
// (F-0451). The login is the API key and token `git credential fill` holds
// for api.trello.com: no secret in .cernerc. Requests go only to that host,
// over https; the key and token travel only in the Authorization header, and
// forge.HTTPDo never follows a redirect.
type trello struct {
	name, board string // board: the board id of the record's URL
}

// trelloAPI is the one host the adapter talks to.
const trelloAPI = "api.trello.com"

// trelloBoardPath is the escaped path of a board URL, /b/<boardId>[/<slug>].
var trelloBoardPath = regexp.MustCompile(`^/b/([A-Za-z0-9]{8})(?:/[^/]+)?/?$`)

func newTrello(c Config) (IssueTracker, error) {
	u, err := url.Parse(c.URL)
	if err == nil && strings.EqualFold(u.Host, "trello.com") && u.RawQuery == "" && u.Fragment == "" { // tracker.New already refused a user and password
		if m := trelloBoardPath.FindStringSubmatch(u.EscapedPath()); m != nil {
			return trello{name: c.Name, board: m[1]}, nil
		}
	}
	return nil, fmt.Errorf("%w %q: want https://trello.com/b/<boardId>/<slug>", ErrTrackerURL, redactURL(c.URL))
}

// trelloCall is one public call: the login is read once.
type trelloCall struct {
	t    trello
	auth string // the Authorization header
}

func (t trello) call(ctx context.Context) (*trelloCall, error) {
	ctx, cancel := context.WithTimeout(ctx, forge.CallTimeout)
	defer cancel()
	key, token := forge.CredentialFill(ctx, trelloAPI)
	if key == "" || token == "" {
		return nil, fmt.Errorf("trello %s: no API key and token from git credential fill; store the key as the username and the token as the password for %s", trelloAPI, trelloAPI)
	}
	return &trelloCall{t: t, auth: `OAuth oauth_consumer_key="` + key + `", oauth_token="` + token + `"`}, nil
}

// do sends one request to path under https://api.trello.com/1 and decodes a
// 2xx answer into out (nil: drop it). Every request carries cerne/<version>
// as its User-Agent. An error names only the host and the status: never the
// transport's text or the answer's, which may echo the credential.
func (c *trelloCall) do(ctx context.Context, method, path string, in, out any) error {
	ctx, cancel := context.WithTimeout(ctx, forge.CallTimeout)
	defer cancel()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in) // strings and bools only: it cannot fail
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+trelloAPI+"/1"+path, body)
	if err != nil {
		return fmt.Errorf("trello %s: bad request", trelloAPI)
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("User-Agent", "cerne/"+version.Version)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := forge.HTTPDo(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("trello %s: %w", trelloAPI, ctx.Err())
		}
		return fmt.Errorf("trello %s: request failed", trelloAPI)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 { // a 3xx too: forge.HTTPDo did not follow it, and neither does this
		return fmt.Errorf("trello %s: HTTP %d", trelloAPI, resp.StatusCode)
	}
	if out != nil && json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out) != nil {
		return fmt.Errorf("trello %s: unreadable answer", trelloAPI)
	}
	return nil
}

// trelloCard is the part of a Trello card the adapter reads.
type trelloCard struct {
	ShortLink string `json:"shortLink"`
	Name      string `json:"name"`
	Desc      string `json:"desc"`
}

func (t trello) ref(c trelloCard) Ref {
	return Ref{Tracker: t.name, ID: c.ShortLink, URL: "https://trello.com/c/" + c.ShortLink}
}

func (t trello) Create(context.Context, Item) (Ref, error) {
	return Ref{}, errors.New("trello: create not implemented")
}

func (t trello) Update(context.Context, Ref, Item) error {
	return errors.New("trello: update not implemented")
}

// ListOpen lists the board's open cards.
func (t trello) ListOpen(ctx context.Context, _ string) ([]Remote, error) {
	c, err := t.call(ctx)
	if err != nil {
		return nil, err
	}
	var cards []trelloCard
	if err := c.do(ctx, http.MethodGet, "/boards/"+t.board+"/cards?filter=open&fields=name,desc,shortLink", nil, &cards); err != nil {
		return nil, err
	}
	var out []Remote
	for _, cd := range cards {
		out = append(out, Remote{Ref: t.ref(cd), Title: cd.Name, Body: cd.Desc})
	}
	return out, nil
}

func (t trello) CommitRef(Ref, string, bool) string { return "" }

func (t trello) Owns(Ref) bool { return false }
