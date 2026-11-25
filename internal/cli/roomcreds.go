package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/henriquesakata/cerne/internal/project"
	"github.com/spf13/cobra"
)

// errNoRoomSession: no flag, no variable and nothing stored for this agent
// session; errNoRoomFacilitator: no flag and no variable (B-0084).
var (
	errNoRoomSession     = errors.New("a session is required: pass --session or set CERNE_ROOM_SESSION (run 'cerne room join <room>' to obtain one; in an agent session, where CERNE_SESSION is set, join stores it for that room and room commands find it on their own)")
	errNoRoomFacilitator = errors.New("a facilitator capability is required: pass --facilitator or set CERNE_ROOM_FACILITATOR (printed once by 'cerne room open' and never stored; a room opened in cerne ui keeps it in that browser)")
)

const (
	roomSessionFlagHelp     = "session capability printed by 'cerne room join' (falls back to CERNE_ROOM_SESSION, then the one join stored for this agent session)"
	roomFacilitatorFlagHelp = "facilitator capability printed by 'cerne room open' (falls back to CERNE_ROOM_FACILITATOR)"
)

// roomCreds is what `room join` keeps for one agent session in one room
// (B-0084), so later room commands find the session without a flag or a
// variable: in an agent harness both sit in the argv of the shell wrapper,
// where the process list, the shell history and transcripts show them. It
// guards against that accidental exposure, not against a process of the same
// user that reads the cache. The facilitator capability is never stored:
// every subagent of the opener's session would hold the role.
type roomCreds struct {
	Nick      string `json:"nick,omitempty"`
	SessionID string `json:"session_id,omitempty"`
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

// saveRoomCreds writes c atomically, 0600 under a 0700 dir; without
// CERNE_SESSION it writes nothing.
func saveRoomCreds(projectID, roomID string, c roomCreds) error {
	path, err := roomCredsPath(projectID, roomID)
	if err != nil || path == "" {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return writePrivateFile(path, data)
}

// resolveRoomSession: --session, then CERNE_ROOM_SESSION, then the session
// `room join` stored for this agent session in roomID.
func resolveRoomSession(flag, projectID, roomID string) (string, error) {
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
	if c.SessionID == "" {
		return "", errNoRoomSession
	}
	return c.SessionID, nil
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

// keepJoinedSession stores the session `room join` just minted for this agent
// session, unless the room already keeps another nick's session that still
// works: a subagent shares its parent's CERNE_SESSION (hookchannelcmd.go), so
// replacing it would hand the parent's nick over. Then kept names that nick
// and nothing is stored. A transient resume failure counts as still working.
func keepJoinedSession(ctx context.Context, c roomClient, roomID, nick, sessionID string) (stored bool, kept string, err error) {
	if path, err := roomCredsPath(c.pid, roomID); err != nil || path == "" {
		return false, "", err
	}
	prev, _ := loadRoomCreds(c.pid, roomID) // unreadable: replaced
	if prev.SessionID != "" && prev.Nick != nick {
		if _, err := c.resume(ctx, roomID, prev.SessionID); err == nil || errors.Is(err, errRoomTransient) {
			return false, prev.Nick, nil
		}
	}
	prev.Nick, prev.SessionID = nick, sessionID
	return true, "", saveRoomCreds(c.pid, roomID, prev)
}

// printKeptOrEnv says where the session lives after join: stored for this
// agent session, or else the env line (no CERNE_SESSION, another nick kept, a
// failed write) — the only copyable form that carries it.
func printKeptOrEnv(cmd *cobra.Command, stored bool, err error, envVar, capability string) {
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not store it for this agent session: %v\n", err)
	}
	if stored && err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), "stored for this agent session (CERNE_SESSION) in a 0600 cache file: room commands for this room find it with no flag or variable")
		return
	}
	fmt.Fprintf(cmd.OutOrStdout(), "env: export %s=%s\n", envVar, capability)
}
