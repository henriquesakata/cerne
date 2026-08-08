package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

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

func handleRoomList(w http.ResponseWriter, projectID string) {
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rooms := m.List()
	if rooms == nil {
		rooms = []room.Room{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

type joinRoomRequest struct {
	Nick string `json:"nick"`
	Bio  string `json:"bio"`
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
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
	}
}

func handleRoomJoin(w http.ResponseWriter, r *http.Request, projectID, id string) {
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

func handleRoomSay(w http.ResponseWriter, r *http.Request, projectID, id string) {
	var req sayRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
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

func handleRoomRead(w http.ResponseWriter, r *http.Request, projectID, id string) {
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
