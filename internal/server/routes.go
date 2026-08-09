package server

import "net/http"

// This file is the single source of truth for the cerne data API routes. The
// three servers — single-project (newMux, used by tests), local multi-project
// (NewUIMulti, `cerne ui`) and remote (NewMultiProject, `cerne serve`) — all
// register the same handlers; only the path prefix and how a request resolves
// to a project differ. Each server injects its own projectBinder and calls
// mountDataRoutes, so adding a route means one entry in dataRoutes().

// scopedHandler is a data-API handler already bound to a resolved project's
// backend locations and id. Every handler in dataRoutes() has this shape.
type scopedHandler func(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string)

// projectBinder turns a scopedHandler into an http.HandlerFunc by resolving the
// project for the request: fixed for the single-project server, by {pid} (from
// a registry, or the cache) for the multi-project and remote servers.
type projectBinder func(scopedHandler) http.HandlerFunc

// dataRoute is one entry in the API table. remote marks the routes the remote
// `cerne serve` exposes — a deliberate subset (case 0022); the local ui and the
// single-project mux expose every route.
type dataRoute struct {
	method  string
	pattern string // suffix after "/api" and the per-server prefix
	remote  bool
	handler scopedHandler
}

// dataRoutes returns the full data API. The remote flag preserves the exact set
// that `cerne serve` exposed before this table existed.
func dataRoutes() []dataRoute {
	return []dataRoute{
		{"GET", "/state", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleState(w, repoDir)
		}},
		{"GET", "/savings", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleSavings(w, repoDir)
		}},
		{"GET", "/stats", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleFlowStats(w, repoDir)
		}},
		{"GET", "/work", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleWork(w, repoDir)
		}},
		{"GET", "/version", true, func(w http.ResponseWriter, _ *http.Request, _, _, _ string) {
			handleVersion(w)
		}},
		{"GET", "/features/{id}", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleFeatureDetail(w, repoDir, r.PathValue("id"))
		}},
		{"POST", "/features", true, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleCreateFeature(w, r, repoDir, projDir)
		}},
		{"POST", "/features/{id}/transition", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleTransition(w, r, repoDir, projDir, r.PathValue("id"))
		}},
		{"POST", "/features/{id}/diagram", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleGenerateDiagram(w, r, repoDir, projDir, r.PathValue("id"))
		}},
		{"PUT", "/features/{id}/docs/{doc}", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleFeatureDocPut(w, r, repoDir, projDir, r.PathValue("id"), r.PathValue("doc"))
		}},
		{"POST", "/clarify/{id}/answer", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleClarifyAnswer(w, r, repoDir, projDir, r.PathValue("id"))
		}},
		{"POST", "/clarify/{id}/correct", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleClarifyCorrect(w, r, repoDir, projDir, r.PathValue("id"))
		}},
		{"PUT", "/features/{id}/parent", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleSetParent(w, r, repoDir, projDir, r.PathValue("id"))
		}},
		{"PUT", "/features/{id}/title", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleRenameTitle(w, r, repoDir, projDir, r.PathValue("id"))
		}},
		{"GET", "/artifacts/{featureID}/{name}", false, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleArtifactGet(w, repoDir, r.PathValue("featureID"), r.PathValue("name"))
		}},
		{"PUT", "/artifacts/{featureID}/{name}", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleArtifactPut(w, r, repoDir, projDir, r.PathValue("featureID"), r.PathValue("name"))
		}},
		{"GET", "/decisions/{id}", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleDecisionDetail(w, repoDir, r.PathValue("id"))
		}},
		{"GET", "/references/{id}", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleReferenceDetail(w, repoDir, r.PathValue("id"))
		}},
		{"GET", "/runbooks/{id}", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleRunbookDetail(w, repoDir, r.PathValue("id"))
		}},
		{"GET", "/notes/{id}", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleNoteDetail(w, repoDir, r.PathValue("id"))
		}},
		{"GET", "/conventions-export", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleConventionsExport(w, repoDir)
		}},
		{"POST", "/references", true, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleCreateReference(w, r, repoDir, projDir)
		}},
		{"GET", "/search", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, projectID string) {
			handleSearch(w, r, repoDir, projectID)
		}},
		{"POST", "/ask", true, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
			handleAsk(w, r, repoDir, projDir, projectID)
		}},
		{"POST", "/chat", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
			handleChat(w, r, repoDir, projDir, projectID)
		}},
		{"POST", "/chat/register", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleChatRegister(w, r, repoDir, projDir)
		}},
		{"GET", "/config/llm", true, func(w http.ResponseWriter, _ *http.Request, _, projDir, _ string) {
			handleLLMConfigGet(w, projDir)
		}},
		{"PUT", "/config/llm", true, func(w http.ResponseWriter, r *http.Request, _, projDir, _ string) {
			handleLLMConfigPut(w, r, projDir)
		}},
		{"GET", "/wiki", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleWikiIndex(w, repoDir)
		}},
		{"GET", "/wiki/{rel...}", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, _ string) {
			handleWikiPage(w, repoDir, r.PathValue("rel"))
		}},
		{"GET", "/lint", true, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
			handleLint(w, r, repoDir, projDir, projectID)
		}},
		{"GET", "/graph", false, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleGraph(w, repoDir)
		}},
		{"GET", "/events", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleEvents(w, repoDir)
		}},
		// War room (T-0001): operational + local to the running server
		// process — never exposed by `cerne serve` (remote=false).
		{"POST", "/rooms", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomOpen(w, r, projectID)
		}},
		{"GET", "/rooms", false, func(w http.ResponseWriter, _ *http.Request, _, _, projectID string) {
			handleRoomList(w, projectID)
		}},
		{"POST", "/rooms/{id}/join", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomJoin(w, r, projectID, r.PathValue("id"))
		}},
		{"POST", "/rooms/{id}/sessions/resume", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomResume(w, r, projectID, r.PathValue("id"))
		}},
		{"POST", "/rooms/{id}/messages", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomSay(w, r, projectID, r.PathValue("id"))
		}},
		{"GET", "/rooms/{id}/messages", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomRead(w, r, projectID, r.PathValue("id"))
		}},
		{"POST", "/rooms/{id}/close", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomClose(w, r, projectID, r.PathValue("id"))
		}},
		// F-0238: the sala->nota vínculo — the note is created in the
		// backend by the CLI (app.CreateNote), this endpoint just records
		// the link on the room that already knows about its own minutes.
		{"POST", "/rooms/{id}/minutes/note", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomSetMinutesNote(w, r, projectID, r.PathValue("id"))
		}},
		// Facilitation (F-0228): a room-scoped capability, separate from
		// participant sessions, that gates pending-item triage, resolution
		// and structured close.
		{"POST", "/rooms/{id}/facilitator/handoff", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomFacilitatorHandoff(w, r, projectID, r.PathValue("id"))
		}},
		{"POST", "/rooms/{id}/items", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomCreateItem(w, r, projectID, r.PathValue("id"))
		}},
		{"GET", "/rooms/{id}/items/pending", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomPending(w, r, projectID, r.PathValue("id"))
		}},
		{"POST", "/rooms/{id}/items/{itemID}/resolve", false, func(w http.ResponseWriter, r *http.Request, _, _, projectID string) {
			handleRoomResolveItem(w, r, projectID, r.PathValue("id"), r.PathValue("itemID"))
		}},
	}
}

// mountDataRoutes registers the data API on mux under prefix ("" for the
// single-project server, "/projects/{pid}" for the scoped servers), binding
// each handler via bind. When remoteOnly, only routes flagged remote are
// mounted — the `cerne serve` subset.
func mountDataRoutes(mux *http.ServeMux, prefix string, bind projectBinder, remoteOnly bool) {
	for _, rt := range dataRoutes() {
		if remoteOnly && !rt.remote {
			continue
		}
		mux.HandleFunc(rt.method+" /api"+prefix+rt.pattern, bind(instrumented(rt)))
	}
}
