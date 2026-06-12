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
