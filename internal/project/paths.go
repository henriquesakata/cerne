package project

import (
	"os"
	"path/filepath"
	"runtime"
)

// CacheDir returns the per-project cerne cache directory:
// <os.UserCacheDir()>/cerne/<projectID>. The env var CERNE_CACHE_DIR, when
// set, overrides the root (yielding <CERNE_CACHE_DIR>/<projectID>) so tests
// stay hermetic. The backend clone lives in <CacheDir>/repo.
func CacheDir(projectID string) (string, error) {
	if root := os.Getenv("CERNE_CACHE_DIR"); root != "" {
		return filepath.Join(root, projectID), nil
	}
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "cerne", projectID), nil
}

// ConfigDir returns the user-level cerne config directory, shared by every
// project and process of this user: <os.UserConfigDir()>/cerne. The env var
// CERNE_CONFIG_DIR, when set, replaces it whole so tests stay hermetic. It
// holds global state such as the model catalog (F-0311).
func ConfigDir() (string, error) {
	if dir := os.Getenv("CERNE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "cerne"), nil
}

// IndexPath returns the project's local search index database path:
// <CacheDir(projectID)>/index.db — a sibling of the repo/ clone, never
// inside it, so the index is rebuildable without dirtying the backend.
func IndexPath(projectID string) (string, error) {
	dir, err := CacheDir(projectID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "index.db"), nil
}

// IndexPathForRepo is the index.db sibling of the repo/ clone, for callers
// that only know the clone's directory (the same file IndexPath names).
func IndexPathForRepo(repoDir string) string {
	return filepath.Join(filepath.Dir(repoDir), "index.db")
}

// BackendDataDir returns where `cerne init` creates a project's local backend
// when no --backend is given: <data root>/cerne/backends/<projectID>.git. It
// lives outside CacheDir on purpose — the cache is derived and deletable, the
// backend is the source of truth (F-0365). CERNE_DATA_DIR, when set, replaces
// the data root whole (yielding <CERNE_DATA_DIR>/backends/<projectID>.git) so
// tests stay hermetic. Otherwise $XDG_DATA_HOME, then ~/.local/share on linux,
// then os.UserConfigDir() elsewhere (macOS Application Support, Windows
// %AppData%) — the stdlib has no user data dir.
func BackendDataDir(projectID string) (string, error) {
	if root := os.Getenv("CERNE_DATA_DIR"); root != "" {
		return filepath.Join(root, "backends", projectID+".git"), nil
	}
	root := os.Getenv("XDG_DATA_HOME")
	if root == "" {
		var err error
		if runtime.GOOS == "linux" {
			var home string
			if home, err = os.UserHomeDir(); err == nil {
				root = filepath.Join(home, ".local", "share")
			}
		} else {
			root, err = os.UserConfigDir()
		}
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(root, "cerne", "backends", projectID+".git"), nil
}
