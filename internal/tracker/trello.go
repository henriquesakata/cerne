package tracker

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/domain"
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

// trelloCall is one public call: the login is read once, and the lists live
// only until the call returns.
type trelloCall struct {
	t       trello
	auth    string                         // the Authorization header
	lists   map[domain.FeatureState]string // state -> list id; nil until resolved
	boardID string                         // the board's long id, as a card's idBoard carries it; "" until resolved
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

// trelloLists are the board's lifecycle lists, in board order. Their names
// are the English state labels of the cerne UI (ui/src/i18n.tsx, state.*),
// copied because the Go core cannot import the UI: the board is the client's,
// and the CLI speaks English (N-0068). rejected and superseded have no list:
// their card is archived where it is.
var trelloLists = []struct {
	state domain.FeatureState
	name  string
}{
	{domain.StateCandidate, "Candidate"},
	{domain.StateApproved, "Approved"},
	{domain.StateInDevelopment, "In development"},
	{domain.StateImplemented, "Ready"},
	{domain.StateDone, trelloDone},
}

// trelloDone is the list of finished cards, which ListOpen leaves out.
const trelloDone = "Done"

// trelloList is the part of a Trello list the adapter reads.
type trelloList struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Pos     float64 `json:"pos"`
	IDBoard string  `json:"idBoard"`
}

// trelloNameKey is a list name with its case and spaces dropped: the form two
// names are compared in.
func trelloNameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), ""))
}

// openLists reads the board's open lists, in board order.
func (c *trelloCall) openLists(ctx context.Context) ([]trelloList, error) {
	var ls []trelloList
	if err := c.do(ctx, http.MethodGet, "/boards/"+c.t.board+"/lists?filter=open&fields=name,pos,idBoard", nil, &ls); err != nil {
		return nil, err
	}
	slices.SortStableFunc(ls, func(a, b trelloList) int { return cmp.Compare(a.Pos, b.Pos) })
	return ls, nil
}

// resolve finds the list of each lifecycle state: the first open list, in
// board order, whose name matches. A list the board lacks (or that a third
// party renamed or archived) is created, at the end of the board, in
// lifecycle order; a duplicate never is. The lists' idBoard is the board's
// long id.
func (c *trelloCall) resolve(ctx context.Context) error {
	ls, err := c.openLists(ctx)
	if err != nil {
		return err
	}
	return c.resolveFrom(ctx, ls)
}

// resolveFrom is resolve over lists already read.
func (c *trelloCall) resolveFrom(ctx context.Context, ls []trelloList) error {
	byKey := map[string]trelloList{}
	for _, l := range ls {
		if _, dup := byKey[trelloNameKey(l.Name)]; !dup {
			byKey[trelloNameKey(l.Name)] = l
		}
		c.boardID = l.IDBoard
	}
	c.lists = map[domain.FeatureState]string{}
	for _, want := range trelloLists {
		l, ok := byKey[trelloNameKey(want.name)]
		if !ok {
			if err := c.do(ctx, http.MethodPost, "/boards/"+c.t.board+"/lists", map[string]string{"name": want.name, "pos": "bottom"}, &l); err != nil {
				return err
			}
		}
		c.lists[want.state] = l.ID
		c.boardID = l.IDBoard
	}
	return nil
}

// trelloCard is the part of a Trello card the adapter reads.
type trelloCard struct {
	ShortLink string        `json:"shortLink"`
	Name      string        `json:"name"`
	Desc      string        `json:"desc"`
	IDList    string        `json:"idList"`
	IDBoard   string        `json:"idBoard"`
	Closed    bool          `json:"closed"` // archived
	Labels    []trelloLabel `json:"labels"`
}

type trelloLabel struct {
	Name string `json:"name"`
}

// trelloCardFields is the ?fields= of every card read.
const trelloCardFields = "fields=name,desc,idList,idBoard,closed,shortLink,labels"

// trelloShortLink is a card's shortLink: the Ref.ID, checked before it goes
// in a path.
var trelloShortLink = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

func (t trello) ref(c trelloCard) Ref {
	return Ref{Tracker: t.name, ID: c.ShortLink, URL: "https://trello.com/c/" + c.ShortLink}
}

// Create adds the card at the bottom of its state's list; a rejected or
// superseded card is added to Candidate, then archived.
func (t trello) Create(ctx context.Context, it Item) (Ref, error) {
	c, err := t.call(ctx)
	if err != nil {
		return Ref{}, err
	}
	if err := c.resolve(ctx); err != nil {
		return Ref{}, err
	}
	var card trelloCard
	list := c.lists[it.State]
	if list == "" {
		list = c.lists[domain.StateCandidate]
	}
	in := map[string]string{"idList": list, "name": it.Title, "desc": it.Body(), "pos": "bottom"}
	if err := c.do(ctx, http.MethodPost, "/cards?"+trelloCardFields, in, &card); err != nil {
		return Ref{}, err
	}
	if !trelloShortLink.MatchString(card.ShortLink) {
		return Ref{}, fmt.Errorf("trello %s: unreadable answer", trelloAPI)
	}
	return t.ref(card), c.apply(ctx, card, it) // a Ref with an error: the card exists, its state did not land
}

// Update reads the card and writes only what differs from it. A card on
// another board is refused: its URL does not name the board, so Owns cannot
// tell a link left by a tracker since re-pointed elsewhere.
func (t trello) Update(ctx context.Context, r Ref, it Item) error {
	if !trelloShortLink.MatchString(r.ID) {
		return fmt.Errorf("%w: card shortLink %q", errBadRef, r.ID)
	}
	c, err := t.call(ctx)
	if err != nil {
		return err
	}
	var card trelloCard
	if err := c.do(ctx, http.MethodGet, "/cards/"+r.ID+"?"+trelloCardFields, nil, &card); err != nil {
		return err
	}
	// Read-only until the card is known to be on this board: resolve may
	// create lists, and a refused update must write nothing.
	ls, err := c.openLists(ctx)
	if err != nil {
		return err
	}
	for _, l := range ls {
		c.boardID = l.IDBoard
	}
	if c.boardID == "" || card.IDBoard != c.boardID {
		return fmt.Errorf("%w: trello card %s is not on board %s", ErrTrackerURL, r.ID, c.t.board)
	}
	if err := c.resolveFrom(ctx, ls); err != nil {
		return err
	}
	card.ShortLink = r.ID
	return c.apply(ctx, card, it)
}

// apply moves cur toward it in one PUT, or none: an open state's list, or
// the archive, where it is, for rejected and superseded. The reason is
// commented only by the call that archives the card, so a sync never repeats
// it. done is the Done list, never the archive.
func (c *trelloCall) apply(ctx context.Context, cur trelloCard, it Item) error {
	edit := map[string]any{}
	if it.Title != "" && cur.Name != it.Title {
		edit["name"] = it.Title
	}
	if it.Title != "" && cur.Desc != it.Body() {
		edit["desc"] = it.Body()
	}
	reason := trelloReason(it)
	if l := c.lists[it.State]; l != "" && cur.IDList != l {
		edit["idList"] = l
	}
	if archive := reason != ""; cur.Closed != archive {
		edit["closed"] = archive
	}
	if len(edit) == 0 {
		return nil
	}
	if err := c.do(ctx, http.MethodPut, "/cards/"+cur.ShortLink, edit, nil); err != nil {
		return err
	}
	if edit["closed"] == true {
		return c.do(ctx, http.MethodPost, "/cards/"+cur.ShortLink+"/actions/comments", map[string]string{"text": reason}, nil)
	}
	return nil
}

// trelloReason is the comment a card state archives its Trello card with; ""
// keeps the card open.
func trelloReason(it Item) string {
	switch {
	case it.State == domain.StateRejected:
		return "Rejected in cerne"
	case it.State == domain.StateSuperseded && it.Successor != nil:
		return "Superseded by " + it.Successor.URL
	case it.State == domain.StateSuperseded:
		return "Superseded in cerne"
	}
	return ""
}

// ListOpen lists up to 100 open cards of the board carrying label ("" = any),
// matched here by name, in the order the board answers them. A card in a Done
// list is finished work, and one on an archived list is out of sight: neither
// is listed. Listing creates no list. ponytail: one read of every open card,
// up to an 8 MiB answer; page the read when a board outgrows it.
func (t trello) ListOpen(ctx context.Context, label string) ([]Remote, error) {
	c, err := t.call(ctx)
	if err != nil {
		return nil, err
	}
	ls, err := c.openLists(ctx)
	if err != nil {
		return nil, err
	}
	shown := map[string]bool{}
	for _, l := range ls {
		shown[l.ID] = trelloNameKey(l.Name) != trelloNameKey(trelloDone)
	}
	var cards []trelloCard
	if err := c.do(ctx, http.MethodGet, "/boards/"+t.board+"/cards?filter=open&"+trelloCardFields, nil, &cards); err != nil {
		return nil, err
	}
	var out []Remote
	for _, cd := range cards {
		if len(out) == 100 {
			break
		}
		labeled := label == "" || slices.ContainsFunc(cd.Labels, func(l trelloLabel) bool { return l.Name == label })
		if shown[cd.IDList] && !cd.Closed && labeled {
			out = append(out, Remote{Ref: t.ref(cd), Title: cd.Name, Body: cd.Desc})
		}
	}
	return out, nil
}

// CommitRef is the card's URL: Trello reads no keyword in a commit message,
// so a closing commit and the code's forge change nothing.
func (t trello) CommitRef(r Ref, _ string, _ bool) string {
	if !trelloShortLink.MatchString(r.ID) {
		return ""
	}
	return "Refs: https://trello.com/c/" + r.ID
}

// trelloCardPath is the escaped path of a card URL, /c/<shortLink>[/<slug>].
var trelloCardPath = regexp.MustCompile(`^/c/([A-Za-z0-9]{8})(?:/[A-Za-z0-9._~-]+)?$`)

// Owns reports whether r has the form of a Trello card URL whose shortLink is
// r.ID: only the plain URL counts, no query or fragment, and the path as
// written, so an escaped %2F never becomes a separator. A card URL does not
// name its board, and Owns is pure: Update, which reads the card, refuses
// one on another board.
func (t trello) Owns(r Ref) bool {
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || strings.ContainsAny(r.URL, "?#") || !strings.EqualFold(u.Host, "trello.com") {
		return false
	}
	m := trelloCardPath.FindStringSubmatch(u.EscapedPath())
	return m != nil && m[1] == r.ID
}
