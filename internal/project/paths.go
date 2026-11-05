package project

import (
	"os"
	"path/filepath"
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
