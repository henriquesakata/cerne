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
	rm, err := m.Open(req.Topic)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, rm)
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
	case errors.Is(err, room.ErrClosed):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, room.ErrNickTaken):
		writeJSON(w, http.StatusConflict, errorBody(err.Error()))
	case errors.Is(err, room.ErrInvalidSession):
		writeJSON(w, http.StatusUnauthorized, errorBody(err.Error()))
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

func handleRoomClose(w http.ResponseWriter, projectID, id string) {
	m, err := roomManagerFor(projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	rm, err := m.Close(id)
	if err != nil {
		writeRoomErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rm)
}
