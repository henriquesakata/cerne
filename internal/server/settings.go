package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/gitback"
	"github.com/henriquesakata/cerne/internal/project"
)

// settingsBroadcast announces a landed settings write (F-0315) or project
// rename (F-0331): the tab gates and the project name ride GET /api/projects,
// so a write changes that payload. The `cerne ui` server injects the SSE hub's
// broadcast, everywhere else no-op.
var settingsBroadcast = func() {}

// handleProjectNamePut renames the project from the local ui (F-0331): writes
// {"name"} to the backend's project.json and answers {"name"} as stored.
func handleProjectNamePut(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !sameOriginJSON(r) {
		writeJSON(w, http.StatusForbidden, errorBody("cross-origin or non-JSON request refused"))
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	name, err := app.RenameProject(repoDir, projDir, req.Name)
	// Every outcome re-reads the list (critic C3): a rename can land and still
	// fail its tail (docs.lock), and a tab must not keep a name the backend
	// does not have. The re-read is idempotent.
	settingsBroadcast()
	if err != nil {
		writeJSON(w, projectNameStatus(err), errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

// projectNameStatus maps a refused rename: a broken rule is the caller's 400; a
// busy lock, a concurrent change or lost push races are a retryable 409; the
// rest is a 500.
func projectNameStatus(err error) int {
	switch {
	case errors.Is(err, app.ErrInvalidProjectName):
		return http.StatusBadRequest
	case errors.Is(err, gitback.ErrLockTimeout), errors.Is(err, app.ErrConflict), errors.Is(err, app.ErrTooConcurrent):
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}

// handleSettingsGet serves the settings registry resolved for projDir
// (F-0314): every key's declaration, effective value and winning scope, plus
// the problems the resolution skipped. Always 200: a bad file is data the
// settings screen shows so the user can fix it. channel.human_name always
// carries the name an empty value signs with (F-0333), from the function the
// channel itself falls back to, so the field can say it even while a name is
// stored.
func handleSettingsGet(w http.ResponseWriter, projDir string) {
	view := project.ResolveSettings(projDir)
	for i, s := range view.Settings {
		if s.ID == "channel.human_name" {
			view.Settings[i].Fallback = app.ChannelFallbackName()
		}
	}
	writeJSON(w, http.StatusOK, view)
}

// handleSettingsPut writes {"scope", "value"} for key id and answers with the
// new view. value is typed JSON checked against the key's type: a bool key
// takes `false`, never the JSON string "false"; a string key takes a string.
func handleSettingsPut(w http.ResponseWriter, r *http.Request, projDir, id string) {
	if !localWrite(w, r) {
		return
	}
	var req struct {
		Scope string `json:"scope"`
		Value any    `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid body: "+err.Error()))
		return
	}
	if err := project.SetSettingValue(projDir, id, req.Scope, req.Value); err != nil {
		writeConfigError(w, err)
		return
	}
	settingsBroadcast()
	handleSettingsGet(w, projDir)
}

// handleSettingsDelete removes key id from ?scope= and answers with the new
// view.
func handleSettingsDelete(w http.ResponseWriter, r *http.Request, projDir, id string) {
	if !localWrite(w, r) {
		return
	}
	// A key the scope does not hold is a no-op here (the CLI says so and
	// exits 1): the UI's unset is idempotent.
	if err := project.UnsetSetting(projDir, id, r.URL.Query().Get("scope")); err != nil && !errors.Is(err, project.ErrSettingNotSet) {
		writeConfigError(w, err)
		return
	}
	settingsBroadcast()
	handleSettingsGet(w, projDir)
}

// localWrite refuses, with 403, a settings write that does not come from the
// local ui (F-0449): the Host must name the loopback, so a page served from a
// name rebound to 127.0.0.1 cannot reach it, and the request must be a
// same-origin JSON one, so a cross-site form or fetch cannot either. Settings
// hold mirror.enabled and mirror.trackers: where cerne publishes.
func localWrite(w http.ResponseWriter, r *http.Request) bool {
	return localWriteOf(w, r, "application/json")
}

// localWriteOf is localWrite for a route whose body is mediaType, not JSON:
// the vault editor's raw markdown (F-0464, audit A4). Same check — loopback
// Host, same-origin request, a body of the route's own type, which a
// cross-site form cannot send.
func localWriteOf(w http.ResponseWriter, r *http.Request, mediaType string) bool {
	if !loopbackHost(r.Host) || !sameOriginAs(r, mediaType) {
		writeJSON(w, http.StatusForbidden, errorBody("this write is accepted only from cerne ui on this machine: loopback host, same-origin "+mediaType+" request"))
		return false
	}
	return true
}

// localOnly refuses, with 403, every request to the local ui whose Host is not
// the loopback (B-0044): a page served from a name rebound to 127.0.0.1 sends
// its own name as Host, and would otherwise read the project and the host's
// ~/.claude layer. A write — any method but GET and HEAD — must also come from
// the page (pageWrite): a cross-site page sends its own Origin (B-0043), curl
// sends none (B-0091). One guard on the router, so a route added later cannot
// forget it; the routes that take a body type check it themselves
// (localWriteOf, sameOriginAs). The remote `cerne serve` is not wrapped: its
// requests come from other hosts by design.
func localOnly(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			writeJSON(w, http.StatusForbidden, errorBody("cerne ui answers only on this machine: loopback host"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			_, pattern := mux.Handler(r)
			if err := pageWrite(r, pattern); err != nil {
				writeJSON(w, http.StatusForbidden, errorBody(err.Error()))
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}

// errNotFromPage refuses a local ui write the cerne ui page did not send
// (B-0091).
var errNotFromPage = errors.New("write refused: not sent by the cerne ui page")

// pageWrite is the local ui's rule for a write to the route pattern (B-0091).
// The server runs as the person who started it, so a data write lands as
// theirs: a bug report born approved, an approve, an accept, a human-only
// setting, a document signed by the human. The page proves itself with its
// Origin, which a browser sends on every write; a request with none, or with
// another one, is errNotFromPage. The exception is a no-Origin write to a
// route cerne's own processes call (cliWrite).
//
// ponytail: the Origin header is the page's whole proof, so a local process
// that sends a matching one (curl -H 'Origin: http://127.0.0.1:7717') still
// writes as the human. A per-process token handed to the page, or a check on
// the peer process, closes that when it matters.
func pageWrite(r *http.Request, pattern string) error {
	o := r.Header.Get("Origin")
	switch {
	case o == "" && cliWrite(pattern):
		return nil
	case o == "":
		return fmt.Errorf("%w: the request has no Origin header, and a write here acts as the human (a bug report, an approve, an accept, a setting) — an agent runs the cerne command instead", errNotFromPage)
	case !sameOrigin(r):
		return fmt.Errorf("%w: Origin %s is not this server", errNotFromPage, o)
	}
	return nil
}

// cliWrite reports a write route cerne's own processes call with no Origin:
// another `cerne ui` registering its project in this panel, deregistering it
// or asking the panel to restart (N-0113), and the `cerne room` client. Two
// room writes still act as the human with no Origin: the channel post with no
// session and the facilitator reset (rooms.go, B-0083).
func cliWrite(pattern string) bool {
	switch pattern {
	case "POST /api/projects", "DELETE /api/projects/{pid}", "POST /api/restart":
		return true
	}
	return strings.Contains(pattern, "/rooms")
}

// loopbackHost reports a Host header naming this machine's loopback
// (localhost, 127.0.0.1 or [::1]), with any port.
func loopbackHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

// writeConfigError answers a refused config write (/settings, /config/llm)
// with the status its sentinel names; anything else is a 500.
func writeConfigError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, project.ErrUnknownSetting):
		status = http.StatusNotFound
	case errors.Is(err, project.ErrInvalidSettingValue), errors.Is(err, project.ErrSettingScope):
		status = http.StatusBadRequest
	case errors.Is(err, project.ErrConfigLayout), errors.Is(err, project.ErrConfigParse):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, project.ErrConfigBusy), errors.Is(err, project.ErrNoProject):
		status = http.StatusConflict
	}
	writeJSON(w, status, errorBody(err.Error()))
}
