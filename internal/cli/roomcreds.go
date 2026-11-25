package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/henriquesakata/cerne/internal/project"
)

// errNoRoomSession: no flag, no variable and nothing stored for this agent
// session; errNoRoomFacilitator: no flag and no variable (B-0084).
// errRoomNickAmbiguous and errRoomNickNotStored: the agent session keeps
// more than one nick in the room and --nick is missing or names none of them.
var (
	errNoRoomSession     = errors.New("a session is required: pass --session or set CERNE_ROOM_SESSION (run 'cerne room join <room>' to obtain one; in an agent session, where CERNE_SESSION is set, join stores it for that room and room commands find it on their own)")
	errNoRoomFacilitator = errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR (printed once by 'cerne room open' and never stored; a room opened in cerne ui keeps it in that browser)")
	errRoomNickAmbiguous = errors.New("this agent session keeps more than one nick in this room: pass --nick <nick> to pick yours")
	errRoomNickNotStored = errors.New("this agent session keeps no session for that nick in this room")
)

const (
	roomSessionFlagHelp     = "session capability printed by 'cerne room join' (falls back to CERNE_ROOM_SESSION, then the one join stored for this agent session)"
	roomNickFlagHelp        = "your nick: picks your session among the ones join stored for this agent session (a selector, not a credential)"
	roomFacilitatorFlagHelp = "facilitator capability printed by 'cerne room open' (falls back to CERNE_ROOM_FACILITATOR)"
)

// roomCreds is what `room join` keeps for one agent session in one room
// (B-0084): each nick joined from it and its session capability, so later
// room commands find the session without a flag or a variable — in an agent
// harness both sit in the argv of the shell wrapper, where the process list,
// the shell history and transcripts show them. A subagent shares its
// parent's CERNE_SESSION (hookchannelcmd.go), so one agent session may hold
// several nicks; --nick picks one. It guards against accidental exposure,
// not against a process of the same user that reads the cache. The
// facilitator capability is never stored: every subagent of the opener's
// session would hold the role.
type roomCreds struct {
	Sessions map[string]string `json:"sessions,omitempty"` // nick → session capability
}

// roomCredsPath is <cache-dir>/room-creds/<room>/<sha256(CERNE_SESSION)>.json,
// or "" without CERNE_SESSION: a human terminal has no agent session, so
// nothing is stored or read for it.
func roomCredsPath(projectID, roomID string) (string, error) {
	agent := os.Getenv("CERNE_SESSION")
	if agent == "" {
		return "", nil
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "room-creds", roomID, hashSessionID(agent)+".json"), nil
}

func loadRoomCreds(projectID, roomID string) (roomCreds, error) {
	path, err := roomCredsPath(projectID, roomID)
	if err != nil || path == "" {
		return roomCreds{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return roomCreds{}, nil
	}
	if err != nil {
		return roomCreds{}, err
	}
	var c roomCreds
	if err := json.Unmarshal(data, &c); err != nil {
		return roomCreds{}, fmt.Errorf("stored room credential %s: %w", path, err)
	}
	return c, nil
}

// storeRoomSession records nick's session for this agent session in roomID
// and returns every nick now stored there, sorted; nil without CERNE_SESSION.
// The read-modify-write holds an OS lock on path+".lock", so parallel joins
// from one agent session (its subagents) never drop each other's entry.
func storeRoomSession(projectID, roomID, nick, sessionID string) ([]string, error) {
	path, err := roomCredsPath(projectID, roomID)
	if err != nil || path == "" {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer lock.Close() // closing drops the lock
	if err := lockFile(lock); err != nil {
		return nil, err
	}
	c, _ := loadRoomCreds(projectID, roomID) // unreadable: replaced
	if c.Sessions == nil {
		c.Sessions = map[string]string{}
	}
	c.Sessions[nick] = sessionID
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	if err := writePrivateFile(path, data); err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(c.Sessions)), nil
}

// resolveRoomSession: --session, then CERNE_ROOM_SESSION, then the session
// `room join` stored for this agent session in roomID — the one of nick, or
// the only one when nick is empty. Several stored nicks and no nick is a
// refusal, never a guess: a subagent would speak as its parent.
func resolveRoomSession(flag, nick, projectID, roomID string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv("CERNE_ROOM_SESSION"); env != "" {
		return env, nil
	}
	c, err := loadRoomCreds(projectID, roomID)
	if err != nil {
		return "", err
	}
	nicks := slices.Sorted(maps.Keys(c.Sessions))
	switch {
	case len(nicks) == 0:
		return "", errNoRoomSession
	case nick != "":
		if s, ok := c.Sessions[nick]; ok {
			return s, nil
		}
		return "", fmt.Errorf("%w: --nick %s (stored: %s)", errRoomNickNotStored, nick, strings.Join(nicks, ", "))
	case len(nicks) == 1:
		return c.Sessions[nicks[0]], nil
	default:
		return "", fmt.Errorf("%w (stored: %s)", errRoomNickAmbiguous, strings.Join(nicks, ", "))
	}
}

// resolveRoomFacilitator: --facilitator, then CERNE_ROOM_FACILITATOR. Never a
// stored one (see roomCreds).
func resolveRoomFacilitator(flag, roomID string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv("CERNE_ROOM_FACILITATOR"); env != "" {
		return env, nil
	}
	return "", fmt.Errorf("%w. Lost it? A human runs 'cerne room facilitator reset %s'", errNoRoomFacilitator, roomID)
}
