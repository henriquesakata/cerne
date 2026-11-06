package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/project"
	"gitlab.com/rockt/cerne/internal/room"
)

// roomReadTimeout is the long-poll wait for GET .../messages?follow=1 (T-0001).
const roomReadTimeout = 25 * time.Second

// roomManagers is the process-wide registry of war-room managers, one per
// project cache dir, reused for the life of the server process so the
// in-memory long-poll wakeup channels work across requests.
var (
	roomManagersMu sync.Mutex
	roomManagers   = map[string]*room.Manager{}
)

// roomManagerFor resolves the room.Manager for projectID, creating it (and
// replaying any existing transcripts) on first use.
func roomManagerFor(projectID string) (*room.Manager, error) {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(cacheDir, "rooms")

	roomManagersMu.Lock()
	defer roomManagersMu.Unlock()
	if m, ok := roomManagers[dir]; ok {
		return m, nil
	}
	m, err := room.NewManager(dir)
	if err != nil {
		return nil, err
	}
	roomManagers[dir] = m
	return m, nil
}

type openRoomRequest struct {
	Topic string `json:"topic"`
}

func handleRoomOpen(w http.ResponseWriter, r *http.Request, projectID string) {
	var req openRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rm, facilitatorID, err := m.Open(req.Topic)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"room": rm, "facilitator_id": facilitatorID})
}

// handleRoomList lists the war rooms behind the permanent channel, which is
// always first (F-0325) — present before its first message, its participants
// the active works.
func handleRoomList(w http.ResponseWriter, repoDir, projectID string) {
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	msgs, roster, err := app.ChannelRead(cacheDir, repoDir, "", time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	channel := room.Room{ID: room.ChannelID, Permanent: true, MessageCount: len(msgs), Participants: []room.Participant{}}
	for _, r := range roster {
		channel.Participants = append(channel.Participants, room.Participant{Nick: r.Work, Bio: r.Title})
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": append([]room.Room{channel}, m.List()...)})
}

// handleChannelRead serves the permanent channel (F-0325): every live message
// with its derived state, plus the channel's version. ?since=N filters like a
// war room read. With follow=1 it long-polls: on ?version=V until the channel
// changes at all (a delivery a hook recorded counts — that is how the UI sees
// "delivered"), without a version until a message past since arrives.
// Reading never records a delivery.
func handleChannelRead(w http.ResponseWriter, r *http.Request, repoDir, projectID string) {
	q := r.URL.Query()
	since, _ := strconv.Atoi(q.Get("since"))
	follow := q.Get("follow") == "1"
	want, err := strconv.ParseInt(q.Get("version"), 10, 64)
	hasVersion := err == nil
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	ch := room.OpenChannel(filepath.Join(cacheDir, "rooms"))
	deadline := time.Now().Add(roomReadTimeout)
	for {
		version := ch.Version()
		all, _, err := app.ChannelRead(cacheDir, repoDir, "", time.Now().UTC())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		out := []app.ChannelMessage{}
		for _, m := range all {
			if m.Seq > since {
				out = append(out, m)
			}
		}
		remaining := time.Until(deadline)
		done := !follow || remaining <= 0 || r.Context().Err() != nil ||
			(hasVersion && version != want) || (!hasVersion && len(out) > 0)
		if done {
			writeJSON(w, http.StatusOK, map[string]any{"messages": out, "version": version})
			return
		}
		ch.Wait(r.Context(), version, remaining)
	}
}

type joinRoomRequest struct {
	Nick string `json:"nick"`
	Bio  string `json:"bio"`
}

// refuseChannelRite answers a war-room rite (join, close, minutes,
// facilitation) aimed at the permanent channel with 409 (F-0325).
func refuseChannelRite(w http.ResponseWriter, id string) bool {
	if id != room.ChannelID {
		return false
	}
	writeRoomErr(w, room.ErrPermanentRoom)
	return true
}

func writeRoomErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, room.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("room not found"))
	case errors.Is(err, room.ErrItemNotFound):
		writeJSON(w, http.StatusNotFound, errorBody(err.Error()))
	case errors.Is(err, room.ErrClosed):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, room.ErrNickTaken):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, room.ErrOpenItems):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, room.ErrInvalidSession):
		writeJSON(w, http.StatusUnauthorized, errorBody(err.Error()))
	case errors.Is(err, room.ErrNotFacilitator):
		writeJSON(w, http.StatusForbidden, errorBody(err.Error()))
	case errors.Is(err, room.ErrIncompleteMinutes):
		writeJSON(w, http.StatusUnprocessableEntity, errorBody(err.Error()))
	case errors.Is(err, room.ErrRoomOpen):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, room.ErrPermanentRoom):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, app.ErrNoChannelIdentity):
		writeJSON(w, http.StatusForbidden, errorBody(err.Error()+" — run `cerne work start \"<what you are doing>\"` in that session first"))
	case errors.Is(err, room.ErrMessageTooLong):
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
	case errors.Is(err, room.ErrChannelBusy):
		writeJSON(w, http.StatusServiceUnavailable, errorBody(err.Error()))
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	}
}

func handleRoomJoin(w http.ResponseWriter, r *http.Request, projectID, id string) {
	if refuseChannelRite(w, id) {
		return
	}
	var req joinRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rm, sess, err := m.Join(id, req.Nick, req.Bio)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"room": rm, "session": sess})
}

type resumeRequest struct {
	SessionID string `json:"session_id"`
}

// handleRoomResume validates an existing capability (F-0226): the session
// comes back without any join event, so a restart/reload retakes the same
// identity.
func handleRoomResume(w http.ResponseWriter, r *http.Request, projectID, id string) {
	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	sess, err := m.Resume(id, req.SessionID)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

type sayRequest struct {
	SessionID string `json:"session_id"`
	Body      string `json:"body"`
}

func handleRoomSay(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID, id string) {
	var req sayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if id == room.ChannelID {
		// F-0325: a post carrying a session is that session's agent, exactly
		// like `cerne msg send` (`cerne room say channel --session` sends
		// one); only a post with no session — the UI composer — is the human,
		// named by the channel.human_name setting.
		// ponytail: local trust model, like every room route — any process
		// that can reach this localhost port may post as the human or as any
		// session id it knows. Authenticate when the server leaves the machine.
		if strings.TrimSpace(req.Body) == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("the message cannot be empty"))
			return
		}
		cacheDir, err := project.CacheDir(projectID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
			return
		}
		now := time.Now().UTC()
		var msg room.Message
		if req.SessionID != "" {
			msg, _, err = app.ChannelSend(cacheDir, repoDir, req.SessionID, req.Body, now)
			msg.Session = "" // a session id is never served
		} else {
			msg, err = app.ChannelPost(cacheDir, app.ChannelHumanName(projDir), req.Body, now)
		}
		if err != nil {
			writeRoomErr(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, msg)
		return
	}
	if req.SessionID == "" {
		// covers the pre-F-0226 payload ({nick, body}): nick is no longer
		// an authority, point the old client at the migration path.
		writeJSON(w, http.StatusUnauthorized, errorBody("a session capability is required — run 'cerne room join' to obtain one"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	msg, err := m.Say(id, req.SessionID, req.Body)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, msg)
}

// roomEventView is one transcript event as served over HTTP. Own is set on
// every event only when the read was authenticated by a session (F-0226
// criterion 10); public reads omit it.
type roomEventView struct {
	room.Message
	Own *bool `json:"own,omitempty"`
}

func handleRoomRead(w http.ResponseWriter, r *http.Request, repoDir, projectID, id string) {
	if id == room.ChannelID {
		handleChannelRead(w, r, repoDir, projectID)
		return
	}
	since, _ := strconv.Atoi(r.URL.Query().Get("since")) // missing/invalid → 0, the full transcript
	follow := r.URL.Query().Get("follow") == "1"

	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	var reader *room.Session
	if token := r.Header.Get("X-Room-Session"); token != "" {
		sess, err := m.Resume(id, token)
		if err != nil {
			writeRoomErr(w, err)
			return
		}
		reader = &sess
	}
	msgs, err := m.Read(r.Context(), id, since, follow, roomReadTimeout)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	out := make([]roomEventView, 0, len(msgs))
	for _, msg := range msgs {
		v := roomEventView{Message: msg}
		if reader != nil {
			own := msg.Nick != "" && msg.Nick == reader.Nick
			v.Own = &own
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": out})
}

type handoffFacilitatorRequest struct {
	FacilitatorID string `json:"facilitator_id"`
}

// handleRoomFacilitatorHandoff rotates the room's facilitator capability
// (F-0228 criterion 1): the presented capability is validated and replaced
// atomically, so the caller can transfer control (e.g. to a human) by
// handing over only the freshly minted id.
func handleRoomFacilitatorHandoff(w http.ResponseWriter, r *http.Request, projectID, id string) {
	if refuseChannelRite(w, id) {
		return
	}
	var req handoffFacilitatorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	next, err := m.HandoffFacilitator(id, req.FacilitatorID)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"facilitator_id": next})
}

type createItemRequest struct {
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"` // proposal|decision_request
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// handleRoomCreateItem raises a proposal or decision-request item on behalf
// of an authenticated participant (F-0228 criterion 2).
func handleRoomCreateItem(w http.ResponseWriter, r *http.Request, projectID, id string) {
	if refuseChannelRite(w, id) {
		return
	}
	var req createItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	var (
		item    room.FacilitationItem
		itemErr error
	)
	switch req.Kind {
	case "proposal":
		item, itemErr = m.Propose(id, req.SessionID, req.Title, req.Body)
	case "decision_request":
		item, itemErr = m.RequestDecision(id, req.SessionID, req.Title, req.Body)
	default:
		writeJSON(w, http.StatusBadRequest, errorBody(`kind must be "proposal" or "decision_request"`))
		return
	}
	if itemErr != nil {
		writeRoomErr(w, itemErr)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleRoomPending lists the room's open facilitation items — the
// facilitator's triage queue, no LLM synthesis (F-0228 criterion 3).
func handleRoomPending(w http.ResponseWriter, r *http.Request, projectID, id string) {
	if refuseChannelRite(w, id) {
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	if err := m.CheckFacilitator(id, r.Header.Get("X-Room-Facilitator")); err != nil {
		writeRoomErr(w, err)
		return
	}
	items, err := m.Pending(id)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	if items == nil {
		items = []room.FacilitationItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type resolveItemRequest struct {
	FacilitatorID string `json:"facilitator_id"`
	Result        string `json:"result"`
}

// handleRoomResolveItem resolves an open facilitation item; only the
// facilitator may (F-0228 criterion 4).
func handleRoomResolveItem(w http.ResponseWriter, r *http.Request, projectID, id, itemID string) {
	if refuseChannelRite(w, id) {
		return
	}
	var req resolveItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	item, err := m.Resolve(id, req.FacilitatorID, itemID, req.Result)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type closeRoomRequest struct {
	FacilitatorID  string       `json:"facilitator_id"`
	Minutes        room.Minutes `json:"minutes"`
	AllowOpenItems bool         `json:"allow_open_items"`
}

func handleRoomClose(w http.ResponseWriter, r *http.Request, projectID, id string) {
	if refuseChannelRite(w, id) {
		return
	}
	var req closeRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rm, err := m.Close(id, req.FacilitatorID, req.Minutes, req.AllowOpenItems)
	if err != nil {
		if errors.Is(err, room.ErrOpenItems) {
			pending, pendErr := m.Pending(id)
			if pendErr == nil {
				writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "pending": pending})
				return
			}
		}
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rm)
}

type setMinutesNoteRequest struct {
	NoteID string `json:"note_id"`
	Force  bool   `json:"force"`
}

// handleRoomSetMinutesNote records the note a closed room's minutes were
// registered as (F-0238 criteria 2-3): refuses an open room, and refuses to
// overwrite an existing link unless Force — reporting the existing note id
// either way, so the CLI can point the caller at it.
func handleRoomSetMinutesNote(w http.ResponseWriter, r *http.Request, projectID, id string) {
	if refuseChannelRite(w, id) {
		return
	}
	var req setMinutesNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	if req.NoteID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("note_id is required"))
		return
	}
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rm, err := m.SetMinutesNote(id, req.NoteID, req.Force)
	if err != nil {
		if errors.Is(err, room.ErrMinutesAlreadyLinked) {
			msg := fmt.Sprintf("room minutes are already registered as %s (pass force to re-register)", rm.MinutesNote)
			writeJSON(w, http.StatusConflict, map[string]any{"error": msg, "minutes_note": rm.MinutesNote})
			return
		}
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rm)
}
