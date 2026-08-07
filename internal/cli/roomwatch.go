package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/rockt/cerne/internal/project"
)

// roomCheckpoint is the durable cursor for one `room watch` consumer
// (F-0227): one file per room+session, so a watcher restart resumes without
// replaying the whole transcript. The session is stored hashed, never raw.
type roomCheckpoint struct {
	RoomID      string    `json:"room_id"`
	SessionHash string    `json:"session_hash"`
	LastSeq     int       `json:"last_seq"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// checkpointPath is <cache-dir>/room-watch/<room>/<sha256(session)>.json —
// a sibling of the "rooms" dir the server itself uses under project.CacheDir.
func checkpointPath(projectID, roomID, sessionID string) (string, error) {
	cacheDir, err := project.CacheDir(projectID)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sessionID))
	return filepath.Join(cacheDir, "room-watch", roomID, hex.EncodeToString(sum[:])+".json"), nil
}

func hashSessionID(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:])
}

func loadCheckpoint(path string) (roomCheckpoint, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return roomCheckpoint{}, false, nil
	}
	if err != nil {
		return roomCheckpoint{}, false, err
	}
	var cp roomCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return roomCheckpoint{}, false, err
	}
	return cp, true, nil
}

// roomCheckpointRename is os.Rename, swappable in tests to simulate a crash
// between the checkpoint write and its atomic commit (spec criterion 8: a
// checkpoint that never lands must leave the previous value in place, so a
// restart repeats the last batch instead of skipping ahead).
var roomCheckpointRename = os.Rename

// saveCheckpoint writes cp atomically (temp file + rename) with 0600
// permissions — the file identifies a room+session pairing, not a secret
// itself (the raw token isn't in it), but there's no reason to make it
// group/world readable.
func saveCheckpoint(path string, cp roomCheckpoint) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := roomCheckpointRename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// errCheckpointLocked is returned when another live `room watch` already
// holds the checkpoint for this room+session (spec criterion 7).
var errCheckpointLocked = errors.New("another 'room watch' is already running for this room and session")

// lockCheckpoint takes a non-blocking exclusive lock on path+".lock": unlike
// gitback.WithLock (which blocks and steals a stale lock for the shared
// backend clone), a second concurrent watcher must fail immediately with a
// clear error rather than wait or steal — two live consumers can't safely
// share one cursor.
//
// ponytail: contention is detected via errors.Is(err, os.ErrExist), which
// covers Linux/macOS/CI. Windows can surface a sharing-violation error
// instead for a live holder (see internal/gitback/lock_windows.go); revisit
// if `cerne room watch` needs to run under Windows CI.
func lockCheckpoint(path string) (func(), error) {
	lockPath := path + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errCheckpointLocked
		}
		return nil, err
	}
	f.Close()
	return func() { os.Remove(lockPath) }, nil
}
