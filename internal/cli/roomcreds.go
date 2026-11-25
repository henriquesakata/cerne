package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	errNoRoomSession      = errors.New("a session is required: pass --session or set CERNE_ROOM_SESSION (run 'cerne room join <room>' to obtain one; in an agent session, where CERNE_SESSION is set, join stores it for that room and room commands find it on their own)")
	errNoRoomFacilitator  = errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR (printed once by 'cerne room open' and never stored; a room opened in cerne ui keeps it in that browser)")
	errRoomNickAmbiguous  = errors.New("this agent session keeps more than one nick in this room: pass --nick <nick> to pick yours")
	errRoomNickNotStored  = errors.New("this agent session keeps no session for that nick in this room")
	errRoomIDNotLocal     = errors.New("invalid room id: it must be a single name like R1, not a path")
	errRoomSessionAndNick = errors.New("pass --session or --nick, not both: --session is the capability itself, --nick picks one this agent session stored")
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
// nothing is stored or read for it. The room comes from the command line, so
// anything but a single local name is refused before it reaches the path.
func roomCredsPath(projectID, roomID string) (string, error) {
	agent := os.Getenv("CERNE_SESSION")
	if agent == "" {
		return "", nil
	}
	if err := checkRoomIDLocal(roomID); err != nil {
		return "", err
	}
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "room-creds", roomID, hashSessionID(agent)+".json"), nil
}

// checkRoomIDLocal refuses a room id that is not a single local name: it
// comes from the command line and names a directory of the stored sessions
// and of the watch cursor.
func checkRoomIDLocal(roomID string) error {
	if !filepath.IsLocal(roomID) || filepath.Base(roomID) != roomID {
		return fmt.Errorf("%w: %q", errRoomIDNotLocal, roomID)
	}
	return nil
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

// resolveRoomSession picks the session a room command speaks with:
// --session; else --nick's entry among the sessions `room join` stored for
// this agent session in roomID, ignoring CERNE_ROOM_SESSION (an agent started
// from a human's shell inherits that variable, and an explicit nick must not
// speak as the human); else CERNE_ROOM_SESSION; else the only stored session.
// --session with --nick, a --nick not stored, or several stored nicks and
// no --nick is a refusal, never a guess.
func resolveRoomSession(flag, nick, projectID, roomID string) (string, error) {
	switch {
	case flag != "" && nick != "":
		return "", errRoomSessionAndNick
	case flag != "":
		return flag, nil
	case nick == "" && os.Getenv("CERNE_ROOM_SESSION") != "":
		return os.Getenv("CERNE_ROOM_SESSION"), nil
	}
	c, err := loadRoomCreds(projectID, roomID)
	if err != nil {
		return "", err
	}
	nicks := slices.Sorted(maps.Keys(c.Sessions))
	if nick != "" {
		if s, ok := c.Sessions[nick]; ok {
			return s, nil
		}
		stored := strings.Join(nicks, ", ")
		if stored == "" {
			stored = "none"
		}
		return "", fmt.Errorf("%w: --nick %s (stored: %s)", errRoomNickNotStored, nick, stored)
	}
	switch {
	case len(nicks) == 0:
		return "", errNoRoomSession
	case len(nicks) == 1:
		return c.Sessions[nicks[0]], nil
	default:
		return "", fmt.Errorf("%w (stored: %s)", errRoomNickAmbiguous, strings.Join(nicks, ", "))
	}
}

// warnRoomSessionVar: join just stored a session, but a CERNE_ROOM_SESSION
// inherited from the shell still wins over a bare room command.
func warnRoomSessionVar(w io.Writer, nick string) {
	if os.Getenv("CERNE_ROOM_SESSION") != "" {
		fmt.Fprintf(w, "warning: CERNE_ROOM_SESSION is set in this environment and overrides bare room commands; pass --nick %s (or unset the variable) to use the session just stored\n", nick)
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
