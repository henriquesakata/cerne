package tracker_test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/forge"
	"github.com/henriquesakata/cerne/internal/tracker"
	"github.com/henriquesakata/cerne/internal/version"
)

// init keeps every test of this package off the network and off the user's
// credential helper: a test that forgets to install the emulator fails
// instead of writing to a real Trello board (F-0451).
func init() {
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("test guard: %s %s must not reach the network (F-0451)", req.Method, req.URL.Host)
	}
	forge.CredentialFill = func(context.Context, string) (string, string) { return "", "" }
}

const (
	trelloKey      = "k3y-f0451"
	trelloToken    = "t0ken-f0451"
	trelloAuth     = `OAuth oauth_consumer_key="k3y-f0451", oauth_token="t0ken-f0451"`
	trelloBoard    = "AbCd1234"
	trelloBoardURL = "https://trello.com/b/AbCd1234/cerne-board"
)

// trelloReq is one request the adapter sent, as it left the adapter.
type trelloReq struct {
	method, url string
	header      http.Header
	body        string
}

// trelloWire is what reached the transport and the credential helper.
type trelloWire struct {
	reqs  []trelloReq
	asked []string // the hosts the credential helper was asked for
}

// installTrello serves h on an httptest.Server and routes forge.HTTPDo to it:
// a request for https://api.trello.com is recorded, then sent to the server
// with its Host kept; any other host is an error. The credential helper
// answers key and token.
func installTrello(t *testing.T, key, token string, h http.Handler) *trelloWire {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	w := &trelloWire{}
	prevDo, prevFill := forge.HTTPDo, forge.CredentialFill
	t.Cleanup(func() { forge.HTTPDo, forge.CredentialFill = prevDo, prevFill })
	forge.CredentialFill = func(_ context.Context, host string) (string, string) {
		w.asked = append(w.asked, host)
		return key, token
	}
	forge.HTTPDo = func(req *http.Request) (*http.Response, error) {
		var body []byte
		if req.Body != nil {
			body, _ = io.ReadAll(req.Body)
		}
		w.reqs = append(w.reqs, trelloReq{req.Method, req.URL.String(), req.Header.Clone(), string(body)})
		if req.URL.Scheme != "https" || req.URL.Host != "api.trello.com" {
			return nil, fmt.Errorf("emulator: %s://%s is not the Trello API", req.URL.Scheme, req.URL.Host)
		}
		out := req.Clone(req.Context())
		out.URL.Scheme, out.URL.Host, out.Host = "http", srv.Listener.Addr().String(), req.URL.Host
		out.Body = io.NopCloser(bytes.NewReader(body))
		return srv.Client().Transport.RoundTrip(out)
	}
	return w
}

// newTrelloTracker builds the adapter named "tt" for the board at boardURL.
func newTrelloTracker(t *testing.T, boardURL string) tracker.IssueTracker {
	t.Helper()
	tr, err := tracker.New(tracker.Config{Name: "tt", Kind: "trello", URL: boardURL})
	if err != nil {
		t.Fatalf("New(trello, %s): %v", boardURL, err)
	}
	return tr
}

// trelloEmpty answers every authorized request with an empty JSON list.
func trelloEmpty() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != trelloAuth {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "[]")
	})
}

// F-0451 crit. 1: kind trello takes only https://trello.com/b/<boardId>/<slug>
// (the slug optional), reads the board id from it without a request, and
// sends every call to that board on the API host.
func TestTrelloURLParsesBoard(t *testing.T) {
	if !slices.Contains(tracker.KindNames(), "trello") {
		t.Fatalf("KindNames() = %v, want it to contain trello", tracker.KindNames())
	}
	for _, raw := range []string{
		"https://trello.example/b/AbCd1234/board", "https://api.trello.com/b/AbCd1234/board", "https://www.trello.com/b/AbCd1234/board",
		"https://127.0.0.1/b/AbCd1234/board", "https://localhost/b/AbCd1234/board", "https://trello.com:8443/b/AbCd1234/board",
		"https://trello.com", "https://trello.com/b/", "https://trello.com/c/AbCd1234/card", "https://trello.com/AbCd1234",
		"https://trello.com/b/AbCd1234/board/extra", "https://trello.com/b/AbCd1234/board?x=1", "https://trello.com/b/AbCd1234/board#x",
		"https://trello.com/b/AbC/board", "https://trello.com/b/AbCd12345/board", "https://trello.com/b/AbCd-234/board",
		"https://trello.com/b/%41bCd1234/board", "https://trello.com/b/../board",
	} {
		if _, err := tracker.New(tracker.Config{Name: "tt", Kind: "trello", URL: raw}); !errors.Is(err, tracker.ErrTrackerURL) {
			t.Errorf("New(trello, %q) = %v, want errors.Is ErrTrackerURL", raw, err)
		}
	}
	w := installTrello(t, trelloKey, trelloToken, trelloEmpty())
	for _, raw := range []string{trelloBoardURL, "https://trello.com/b/AbCd1234", "https://Trello.com/b/AbCd1234/cerne-board/"} {
		tr := newTrelloTracker(t, raw)
		if len(w.reqs) != 0 {
			t.Fatalf("New(trello, %s) sent %d requests, want none", raw, len(w.reqs))
		}
		if _, err := tr.ListOpen(context.Background(), ""); err != nil {
			t.Fatalf("ListOpen on %s: %v", raw, err)
		}
		for _, req := range w.reqs {
			if u, _ := url.Parse(req.url); u.Host != "api.trello.com" || !strings.HasPrefix(u.Path, "/1/boards/"+trelloBoard+"/") {
				t.Fatalf("%s %s: want a call on board %s at api.trello.com", req.method, req.url, trelloBoard)
			}
		}
		w.reqs = nil
	}
}

// F-0451 crit. 1: the login is what `git credential fill` holds for
// api.trello.com, the API key as the user and the token as the password; it
// travels only in "Authorization: OAuth", never in the URL, the body, another
// header or an error. A missing key or token is a clear error, before any
// request.
func TestTrelloAuthInHeaderNotURL(t *testing.T) {
	ctx := context.Background()
	w := installTrello(t, trelloKey, trelloToken, trelloEmpty())
	if _, err := newTrelloTracker(t, trelloBoardURL).ListOpen(ctx, "x"); err != nil {
		t.Fatalf("ListOpen: %v", err)
	}
	if !slices.Equal(w.asked, []string{"api.trello.com"}) {
		t.Fatalf("credential helper asked for %v, want [api.trello.com] once", w.asked)
	}
	if len(w.reqs) == 0 {
		t.Fatal("ListOpen sent no request")
	}
	for i, req := range w.reqs {
		if got := req.header.Get("Authorization"); got != trelloAuth {
			t.Errorf("request %d Authorization = %q, want %q", i, got, trelloAuth)
		}
		if ua := req.header.Get("User-Agent"); ua != "cerne/"+version.Version {
			t.Errorf("request %d User-Agent = %q, want cerne/%s", i, ua, version.Version)
		}
		rest := req.header.Clone()
		rest.Del("Authorization")
		if leak := fmt.Sprint(req.url, rest, req.body); strings.Contains(leak, trelloKey) || strings.Contains(leak, trelloToken) {
			t.Errorf("request %d carries the key or token outside Authorization: %s", i, leak)
		}
	}

	for _, c := range []struct{ name, key, token string }{{"none", "", ""}, {"key only", trelloKey, ""}, {"token only", "", trelloToken}} {
		t.Run("missing credential, "+c.name, func(t *testing.T) {
			w := installTrello(t, c.key, c.token, trelloEmpty())
			_, err := newTrelloTracker(t, trelloBoardURL).ListOpen(ctx, "")
			if err == nil || !strings.Contains(err.Error(), "api.trello.com") || !strings.Contains(err.Error(), "git credential fill") {
				t.Fatalf("ListOpen without a key and token = %v, want an error naming api.trello.com and git credential fill", err)
			}
			if c.key != "" && strings.Contains(err.Error(), c.key) {
				t.Fatalf("error %q carries the key", err)
			}
			if len(w.reqs) != 0 {
				t.Fatalf("%d requests without a credential, want none", len(w.reqs))
			}
		})
	}

	t.Run("an error names only host and status", func(t *testing.T) {
		installTrello(t, trelloKey, trelloToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid token "+trelloToken+" for key "+trelloKey, http.StatusUnauthorized)
		}))
		_, err := newTrelloTracker(t, trelloBoardURL).ListOpen(ctx, "")
		if err == nil || !strings.Contains(err.Error(), "api.trello.com") || !strings.Contains(err.Error(), "401") {
			t.Fatalf("ListOpen on a 401 = %v, want an error naming api.trello.com and 401", err)
		}
		if strings.Contains(err.Error(), trelloToken) || strings.Contains(err.Error(), trelloKey) {
			t.Fatalf("error %q carries the credential", err)
		}
	})
}

// F-0451 crit. 1: a 3xx is an answer, not a hop: one request, an error, and
// the credential neither in the error nor sent to the redirect's host.
func TestTrelloNoRedirectKeepsCredential(t *testing.T) {
	w := installTrello(t, trelloKey, trelloToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	}))
	_, err := newTrelloTracker(t, trelloBoardURL).ListOpen(context.Background(), "")
	if err == nil {
		t.Fatal("ListOpen after a 302 = nil error, want an error")
	}
	if strings.Contains(err.Error(), trelloToken) || strings.Contains(err.Error(), trelloKey) {
		t.Fatalf("error %q carries the credential", err)
	}
	if len(w.reqs) != 1 {
		t.Fatalf("%d requests after a 302, want exactly 1: %v", len(w.reqs), w.reqs)
	}
	if u, _ := url.Parse(w.reqs[0].url); u.Host != "api.trello.com" {
		t.Fatalf("request went to %s, want api.trello.com", u.Host)
	}
}

// trelloFakeList and trelloFakeCard are what the emulator holds; Board is the
// board's long id, as the API's idBoard carries it.
type trelloFakeList struct {
	ID, Name, Board string
	Pos             float64
	Closed          bool
}

type trelloFakeCard struct {
	ID, ShortLink, Name, Desc, List, Board string
	Closed                                 bool
	Labels                                 []string
	Comments                               []string
}

// trelloFake emulates, on an httptest.Server behind forge.HTTPDo, the slice
// of the Trello REST API the adapter calls: a board's lists and open cards,
// a card by shortLink, its edits and its comments. A board is addressed by
// its shortLink and answers its long id as idBoard; a field the adapter does
// not ask for in ?fields= is not in the answer. It refuses a request without
// the OAuth header or the cerne/ User-Agent, and one that carries the key or
// the token anywhere else.
type trelloFake struct {
	boards map[string]string // shortLink -> long id
	lists  []*trelloFakeList
	cards  []*trelloFakeCard
	wire   *trelloWire
	n      int // ids minted
}

// trelloOther is a second board on the same account.
const trelloOther = "ZyXw9876"

func newTrelloFake(t *testing.T) *trelloFake {
	t.Helper()
	f := &trelloFake{boards: map[string]string{}}
	for _, b := range []string{trelloBoard, trelloOther} {
		f.boards[b] = f.id()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /1/boards/{board}/lists", f.onBoard(f.getLists))
	mux.HandleFunc("POST /1/boards/{board}/lists", f.onBoard(f.addList))
	mux.HandleFunc("GET /1/boards/{board}/cards", f.onBoard(f.getCards))
	mux.HandleFunc("POST /1/cards", f.addCard)
	mux.HandleFunc("GET /1/cards/{card}", f.onCard(func(w http.ResponseWriter, r *http.Request, c *trelloFakeCard) {
		trelloReply(w, f.view(c, r))
	}))
	mux.HandleFunc("PUT /1/cards/{card}", f.onCard(f.editCard))
	mux.HandleFunc("POST /1/cards/{card}/actions/comments", f.onCard(f.comment))
	f.wire = installTrello(t, trelloKey, trelloToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		rest := r.Header.Clone()
		rest.Del("Authorization")
		switch leak := fmt.Sprint(r.URL, rest, string(body)); {
		case r.Host != "api.trello.com":
			http.Error(w, "unknown host", http.StatusBadGateway)
		case r.Header.Get("Authorization") != trelloAuth:
			http.Error(w, "unauthorized permission requested", http.StatusUnauthorized)
		case r.Header.Get("User-Agent") != "cerne/"+version.Version:
			http.Error(w, "user agent", http.StatusForbidden)
		case strings.Contains(leak, trelloKey) || strings.Contains(leak, trelloToken):
			http.Error(w, "credential outside the Authorization header", http.StatusBadRequest)
		case len(body) > 0 && r.Header.Get("Content-Type") != "application/json":
			http.Error(w, "want a JSON body", http.StatusUnsupportedMediaType)
		default:
			mux.ServeHTTP(w, r)
		}
	}))
	return f
}

func (f *trelloFake) id() string { f.n++; return fmt.Sprintf("%024x", f.n) }

func trelloReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// onBoard resolves {board} by shortLink, as the API does.
func (f *trelloFake) onBoard(h func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		long, ok := f.boards[r.PathValue("board")]
		if !ok {
			http.Error(w, "invalid id", http.StatusNotFound)
			return
		}
		h(w, r, long)
	}
}

func (f *trelloFake) onCard(h func(http.ResponseWriter, *http.Request, *trelloFakeCard)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := f.card(r.PathValue("card"))
		if c == nil {
			http.Error(w, "The requested resource was not found.", http.StatusNotFound)
			return
		}
		h(w, r, c)
	}
}

func trelloBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return nil, false
	}
	return in, true
}

// openLists is the board's open lists in board order.
func (f *trelloFake) openLists(board string) []*trelloFakeList {
	var out []*trelloFakeList
	for _, l := range f.lists {
		if l.Board == board && !l.Closed {
			out = append(out, l)
		}
	}
	slices.SortStableFunc(out, func(a, b *trelloFakeList) int { return cmp.Compare(a.Pos, b.Pos) })
	return out
}

func (f *trelloFake) getLists(w http.ResponseWriter, r *http.Request, board string) {
	if r.URL.Query().Get("filter") != "open" {
		http.Error(w, "the emulator serves filter=open only", http.StatusBadRequest)
		return
	}
	out := []map[string]any{}
	for _, l := range f.openLists(board) {
		out = append(out, trelloFields(r, map[string]any{"id": l.ID, "name": l.Name, "pos": l.Pos, "idBoard": l.Board, "closed": l.Closed}))
	}
	trelloReply(w, out)
}

func (f *trelloFake) addList(w http.ResponseWriter, r *http.Request, board string) {
	in, ok := trelloBody(w, r)
	if !ok {
		return
	}
	name, _ := in["name"].(string)
	if name == "" || in["pos"] != "bottom" {
		http.Error(w, "want a name and pos bottom", http.StatusBadRequest)
		return
	}
	l := f.addOpenList(board, name)
	trelloReply(w, map[string]any{"id": l.ID, "name": l.Name, "pos": l.Pos, "idBoard": l.Board, "closed": false})
}

// addOpenList puts an open list named name at the bottom of board (a long id).
func (f *trelloFake) addOpenList(board, name string) *trelloFakeList {
	pos := 16384.0
	for _, l := range f.lists {
		if l.Board == board {
			pos = max(pos, l.Pos+16384)
		}
	}
	l := &trelloFakeList{ID: f.id(), Name: name, Board: board, Pos: pos}
	f.lists = append(f.lists, l)
	return l
}

func (f *trelloFake) getCards(w http.ResponseWriter, r *http.Request, board string) {
	if r.URL.Query().Get("filter") != "open" {
		http.Error(w, "the emulator serves filter=open only", http.StatusBadRequest)
		return
	}
	out := []map[string]any{}
	for _, c := range f.cards {
		if c.Board == board && !c.Closed {
			out = append(out, f.view(c, r))
		}
	}
	trelloReply(w, out)
}

func (f *trelloFake) addCard(w http.ResponseWriter, r *http.Request) {
	in, ok := trelloBody(w, r)
	if !ok {
		return
	}
	l := f.list(fmt.Sprint(in["idList"]))
	if l == nil {
		http.Error(w, "invalid value for idList", http.StatusBadRequest)
		return
	}
	name, _ := in["name"].(string)
	desc, _ := in["desc"].(string)
	trelloReply(w, f.view(f.addCardOn(l, name, desc), r))
}

func (f *trelloFake) addCardOn(l *trelloFakeList, name, desc string, labels ...string) *trelloFakeCard {
	c := &trelloFakeCard{ID: f.id(), Name: name, Desc: desc, List: l.ID, Board: l.Board, Labels: labels}
	c.ShortLink = fmt.Sprintf("c%07d", f.n)
	f.cards = append(f.cards, c)
	return c
}

func (f *trelloFake) editCard(w http.ResponseWriter, r *http.Request, c *trelloFakeCard) {
	in, ok := trelloBody(w, r)
	if !ok {
		return
	}
	for k, v := range in {
		switch k {
		case "idList":
			l := f.list(fmt.Sprint(v))
			if l == nil {
				http.Error(w, "invalid value for idList", http.StatusBadRequest)
				return
			}
			c.List, c.Board = l.ID, l.Board
		case "closed":
			b, ok := v.(bool)
			if !ok {
				http.Error(w, "invalid value for closed", http.StatusBadRequest)
				return
			}
			c.Closed = b
		case "name":
			c.Name, _ = v.(string)
		case "desc":
			c.Desc, _ = v.(string)
		default:
			http.Error(w, "the emulator does not edit "+k, http.StatusBadRequest)
			return
		}
	}
	trelloReply(w, f.view(c, r))
}

func (f *trelloFake) comment(w http.ResponseWriter, r *http.Request, c *trelloFakeCard) {
	in, ok := trelloBody(w, r)
	if !ok {
		return
	}
	text, _ := in["text"].(string)
	if text == "" {
		http.Error(w, "invalid value for text", http.StatusBadRequest)
		return
	}
	c.Comments = append(c.Comments, text)
	trelloReply(w, map[string]any{"id": f.id(), "type": "commentCard"})
}

// view is c as the API shows it: every field, cut to ?fields= when asked.
func (f *trelloFake) view(c *trelloFakeCard, r *http.Request) map[string]any {
	labels := []map[string]any{}
	for _, l := range c.Labels {
		labels = append(labels, map[string]any{"id": f.id(), "idBoard": c.Board, "name": l, "color": "green"})
	}
	return trelloFields(r, map[string]any{
		"id": c.ID, "shortLink": c.ShortLink, "shortUrl": "https://trello.com/c/" + c.ShortLink,
		"url": "https://trello.com/c/" + c.ShortLink + "/1-slug", "name": c.Name, "desc": c.Desc,
		"idList": c.List, "idBoard": c.Board, "closed": c.Closed, "labels": labels,
	})
}

// trelloFields keeps the id and the ?fields= of r, all when none is asked.
func trelloFields(r *http.Request, all map[string]any) map[string]any {
	want := r.URL.Query().Get("fields")
	if want == "" || want == "all" {
		return all
	}
	out := map[string]any{"id": all["id"]}
	for _, k := range strings.Split(want, ",") {
		if v, ok := all[k]; ok {
			out[k] = v
		}
	}
	return out
}

func (f *trelloFake) list(id string) *trelloFakeList {
	for _, l := range f.lists {
		if l.ID == id {
			return l
		}
	}
	return nil
}

// card finds a card by shortLink or long id, as the API does.
func (f *trelloFake) card(id string) *trelloFakeCard {
	for _, c := range f.cards {
		if c.ShortLink == id || c.ID == id {
			return c
		}
	}
	return nil
}

// at is the name of the list c is in.
func (f *trelloFake) at(c *trelloFakeCard) string { return f.list(c.List).Name }

// listNames is the board's open lists, by name, in board order.
func (f *trelloFake) listNames(board string) []string {
	var out []string
	for _, l := range f.openLists(f.boards[board]) {
		out = append(out, l.Name)
	}
	return out
}

// count is the number of requests sent with method on a path ending in suffix.
func (f *trelloFake) count(method, suffix string) int {
	n := 0
	for _, req := range f.wire.reqs {
		if u, _ := url.Parse(req.url); req.method == method && strings.HasSuffix(u.Path, suffix) {
			n++
		}
	}
	return n
}

// trelloStateList is the list each open state's card sits in.
var trelloStateList = map[domain.FeatureState]string{
	domain.StateCandidate: "Candidate", domain.StateApproved: "Approved", domain.StateInDevelopment: "In development",
	domain.StateImplemented: "Ready", domain.StateDone: "Done",
}

var trelloLifecycle = []string{"Candidate", "Approved", "In development", "Ready", "Done"}

func trelloCreate(t *testing.T, tr tracker.IssueTracker, it tracker.Item) tracker.Ref {
	t.Helper()
	r, err := tr.Create(context.Background(), it)
	if err != nil {
		t.Fatalf("Create(%s, %s): %v", it.CardID, it.State, err)
	}
	return r
}

func trelloUpdate(t *testing.T, tr tracker.IssueTracker, r tracker.Ref, it tracker.Item) {
	t.Helper()
	if err := tr.Update(context.Background(), r, it); err != nil {
		t.Fatalf("Update(%s, %s): %v", r.ID, it.State, err)
	}
}

// F-0451 crit. 2: a card's Trello card sits in the list of its state, at
// Create and after every Update, and an update with nothing new writes
// nothing.
func TestTrelloMovesToStateList(t *testing.T) {
	fk := newTrelloFake(t)
	tr := newTrelloTracker(t, trelloBoardURL)
	for _, s := range []domain.FeatureState{domain.StateCandidate, domain.StateApproved, domain.StateInDevelopment, domain.StateImplemented, domain.StateDone} {
		r := trelloCreate(t, tr, publicItem("F-0001", s))
		c := fk.card(r.ID)
		if c == nil || fk.at(c) != trelloStateList[s] || c.Closed || c.Name != "Public title of F-0001" || c.Desc != publicItem("F-0001", s).Body() {
			t.Fatalf("Create(%s) left card %+v, want it open in list %s with the public title and body", s, c, trelloStateList[s])
		}
	}
	r := trelloCreate(t, tr, publicItem("F-0002", domain.StateCandidate))
	for _, s := range []domain.FeatureState{domain.StateApproved, domain.StateInDevelopment, domain.StateImplemented, domain.StateDone, domain.StateInDevelopment} {
		trelloUpdate(t, tr, r, publicItem("F-0002", s))
		if c := fk.card(r.ID); fk.at(c) != trelloStateList[s] || c.Closed {
			t.Fatalf("after Update(%s) card is in list %s (closed %v), want %s, open", s, fk.at(c), c.Closed, trelloStateList[s])
		}
	}
	before := len(fk.wire.reqs)
	trelloUpdate(t, tr, r, publicItem("F-0002", domain.StateInDevelopment))
	for _, req := range fk.wire.reqs[before:] {
		if req.method != http.MethodGet {
			t.Fatalf("an unchanged update sent %s %s", req.method, req.url)
		}
	}
}

// F-0451 crit. 2: the lifecycle lists the board lacks are created once, in
// lifecycle order, at the end of the board after the client's own lists.
func TestTrelloCreatesMissingLists(t *testing.T) {
	fk := newTrelloFake(t)
	fk.addOpenList(fk.boards[trelloBoard], "Inbox")
	tr := newTrelloTracker(t, trelloBoardURL)
	trelloCreate(t, tr, publicItem("F-0001", domain.StateApproved))
	if want := append([]string{"Inbox"}, trelloLifecycle...); !slices.Equal(fk.listNames(trelloBoard), want) {
		t.Fatalf("board lists = %q, want %q", fk.listNames(trelloBoard), want)
	}
	if n := fk.count(http.MethodPost, "/lists"); n != 5 {
		t.Fatalf("%d lists created, want 5", n)
	}
	trelloCreate(t, tr, publicItem("F-0002", domain.StateDone))
	if n := fk.count(http.MethodPost, "/lists"); n != 5 {
		t.Fatalf("the second Create created lists again: %d in all, want 5", n)
	}
	if other := fk.listNames(trelloOther); len(other) != 0 {
		t.Fatalf("lists created on another board: %q", other)
	}
}

// F-0451 crit. 2: a list name matches whatever its case and spaces; when two
// lists match, the first in board order wins, and none is created for it.
func TestTrelloListNameCollision(t *testing.T) {
	fk := newTrelloFake(t)
	b := fk.boards[trelloBoard]
	first := fk.addOpenList(b, " in  DEVELOPMENT ")
	fk.addOpenList(b, "In development")
	fk.addOpenList(b, "candidate")
	tr := newTrelloTracker(t, trelloBoardURL)
	r := trelloCreate(t, tr, publicItem("F-0001", domain.StateInDevelopment))
	if c := fk.card(r.ID); c.List != first.ID {
		t.Fatalf("card in list %q (%s), want the first matching one %q (%s)", fk.at(c), c.List, first.Name, first.ID)
	}
	if want := []string{" in  DEVELOPMENT ", "In development", "candidate", "Approved", "Ready", "Done"}; !slices.Equal(fk.listNames(trelloBoard), want) {
		t.Fatalf("board lists = %q, want %q", fk.listNames(trelloBoard), want)
	}
}

// F-0451 crit. 2: a lifecycle list a third party renamed or archived counts
// as missing: the adapter creates it again, and the card moves there on its
// next Update.
func TestTrelloRenamedListIsRecreated(t *testing.T) {
	fk := newTrelloFake(t)
	tr := newTrelloTracker(t, trelloBoardURL)
	a := trelloCreate(t, tr, publicItem("F-0001", domain.StateApproved))
	r := trelloCreate(t, tr, publicItem("F-0002", domain.StateImplemented))
	fk.list(fk.card(a.ID).List).Name = "Approved by the client"
	fk.list(fk.card(r.ID).List).Closed = true

	trelloUpdate(t, tr, a, publicItem("F-0001", domain.StateApproved))
	trelloUpdate(t, tr, r, publicItem("F-0002", domain.StateImplemented))
	if got, want := fk.listNames(trelloBoard), []string{"Candidate", "Approved by the client", "In development", "Done", "Approved", "Ready"}; !slices.Equal(got, want) {
		t.Fatalf("board lists = %q, want %q", got, want)
	}
	for _, c := range []struct {
		r    tracker.Ref
		want string
	}{{a, "Approved"}, {r, "Ready"}} {
		if card := fk.card(c.r.ID); fk.at(card) != c.want || fk.list(card.List).Closed {
			t.Fatalf("card %s is in list %q, want the open %q", c.r.ID, fk.at(card), c.want)
		}
	}
}

// F-0451 crit. 5: Owns is pure and checks the form only: an https card URL on
// trello.com, /c/<shortLink> with an optional slug, whose shortLink is the
// ref's ID; no query or fragment, and the path as written. The card's board
// is checked by Update, which reads the card.
func TestTrelloOwns(t *testing.T) {
	w := installTrello(t, trelloKey, trelloToken, trelloEmpty())
	tr := newTrelloTracker(t, trelloBoardURL)
	for _, u := range []string{"https://trello.com/c/AbCd1234", "https://trello.com/c/AbCd1234/12-some-card", "https://TRELLO.com/c/AbCd1234"} {
		if r := (tracker.Ref{Tracker: "tt", ID: "AbCd1234", URL: u}); !tr.Owns(r) {
			t.Errorf("Owns(%+v) = false, want true", r)
		}
	}
	for _, r := range []tracker.Ref{
		{Tracker: "tt", ID: "AbCd1234", URL: "https://elsewhere.invalid/c/AbCd1234"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://api.trello.com/c/AbCd1234"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com:8443/c/AbCd1234"},
		{Tracker: "tt", ID: "AbCd1234", URL: "http://trello.com/c/AbCd1234"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/b/AbCd1234/board"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/c/AbCd1234?x=1"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/c/AbCd1234#x"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/c/AbCd1234/slug/extra"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/c/AbCd1234%2Fslug"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/c/%41bCd1234"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://trello.com/c/Zz999999"},
		{Tracker: "tt", ID: "AbCd", URL: "https://trello.com/c/AbCd"},
		{Tracker: "tt", ID: "AbCd1234", URL: "https://user@trello.com/c/AbCd1234"},
	} {
		if tr.Owns(r) {
			t.Errorf("Owns(%+v) = true, want false", r)
		}
	}
	if len(w.reqs) != 0 || len(w.asked) != 0 {
		t.Fatalf("Owns sent %d requests and asked for %d credentials, want none", len(w.reqs), len(w.asked))
	}
}
