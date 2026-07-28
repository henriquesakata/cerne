package server

import (
	"os"
	"strings"
)

// serveProjectsEnv names the env that declares which projects this service
// serves and where their backends live.
const serveProjectsEnv = "CERNE_SERVE_PROJECTS"

// configuredBackends parses CERNE_SERVE_PROJECTS — "pid=<git url>" entries
// separated by commas or whitespace, e.g.
//
//	rockt-hub=https://user:token@gitlab.com/rockt/hub-docs.git
//
// Only the FIRST "=" splits, so a credentialed URL carrying "=" in its query
// survives. Malformed entries are skipped rather than failing the service:
// this is a convenience over PUT /api/projects/{pid}, never a precondition.
func configuredBackends() map[string]string {
	raw := os.Getenv(serveProjectsEnv)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := map[string]string{}
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == ' ' || r == '\t'
	}) {
		pid, backend, ok := strings.Cut(entry, "=")
		pid, backend = strings.TrimSpace(pid), strings.TrimSpace(backend)
		if !ok || pid == "" || backend == "" {
			continue
		}
		out[pid] = backend
	}
	return out
}

// ensureConfiguredProject clones pid's backend when the project is declared in
// CERNE_SERVE_PROJECTS but has no clone yet. It exists because the service's
// cache is ephemeral (an emptyDir in k8s): after a pod is rescheduled the
// registration made by an earlier PUT is gone, and every read answered 404
// until a human re-registered — the channel died silently on a restart.
// Reports whether pid is registered after the attempt.
func ensureConfiguredProject(pid string) bool {
	backend, ok := configuredBackends()[pid]
	if !ok {
		return false
	}
	repoDir, projDir, err := projectPaths(pid)
	if err != nil {
		return false
	}
	if err := ensureProjectClone(repoDir, projDir, backend, pid, nil); err != nil {
		return false // best-effort: the next read retries
	}
	return true
}
