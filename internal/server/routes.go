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
		{"PUT", "/features/{id}/parent", false, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleSetParent(w, r, repoDir, projDir, r.PathValue("id"))
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
		{"POST", "/references", true, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, _ string) {
			handleCreateReference(w, r, repoDir, projDir)
		}},
		{"GET", "/search", true, func(w http.ResponseWriter, r *http.Request, repoDir, _, projectID string) {
			handleSearch(w, r, repoDir, projectID)
		}},
		{"POST", "/ask", true, func(w http.ResponseWriter, r *http.Request, repoDir, projDir, projectID string) {
			handleAsk(w, r, repoDir, projDir, projectID)
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
		{"GET", "/lint", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleLint(w, repoDir)
		}},
		{"GET", "/graph", false, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleGraph(w, repoDir)
		}},
		{"GET", "/events", true, func(w http.ResponseWriter, _ *http.Request, repoDir, _, _ string) {
			handleEvents(w, repoDir)
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
		mux.HandleFunc(rt.method+" /api"+prefix+rt.pattern, bind(rt.handler))
	}
}
