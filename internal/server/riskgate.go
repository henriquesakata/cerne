package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/henriquesakata/cerne/internal/app"
	"github.com/henriquesakata/cerne/internal/domain"
	"github.com/henriquesakata/cerne/internal/store"
)

// The risk gate's learning in the local ui (F-0518). Every write is the
// owner's: the person at the form, behind localWrite (loopback host,
// same-origin JSON). B-0091: a request with no Origin still passes as the
// person, as on every other local write route.

// riskFeedbackRequest is the POST /api/features/{id}/risk-feedback body.
type riskFeedbackRequest struct {
	Answer    string   `json:"answer"`
	Reason    string   `json:"reason"`
	AddPaths  []string `json:"add_paths"`
	AddFacets []string `json:"add_facets"`
}

// handleRiskFeedback records the owner's answer on a card the risk gate
// decided and answers with the decision.
func handleRiskFeedback(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	if !localWrite(w, r) {
		return
	}
	var req riskFeedbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Answer != app.FeedbackShouldHaveAsked && req.Answer != app.FeedbackNotNeeded {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body: answer is "+app.FeedbackShouldHaveAsked+" or "+app.FeedbackNotNeeded))
		return
	}
	d, err := app.RecordRiskFeedback(repoDir, projDir, id, app.RiskFeedback{Answer: req.Answer, Reason: req.Reason, AddPaths: req.AddPaths, AddFacets: req.AddFacets})
	if err != nil {
		writeRiskError(w, err)
		return
	}
	settingsBroadcast() // the lists may have changed
	writeJSON(w, http.StatusOK, d)
}

// handleAcceptMode stores the owner's mark on a card not yet concluded.
func handleAcceptMode(w http.ResponseWriter, r *http.Request, repoDir, projDir, id string) {
	if !localWrite(w, r) {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body"))
		return
	}
	f, err := app.SetFeatureAcceptMode(repoDir, projDir, id, req.Mode)
	if err != nil {
		writeRiskError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, createFeatureResponse{Feature: f})
}

// handleRiskGate serves what the risk gate learned: the calibration, the
// questions, the open proposals and the counts of the last ?days= days (30
// by default).
func handleRiskGate(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	days := 30
	if s := r.URL.Query().Get("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusBadRequest, errorBody("days: want a positive integer"))
			return
		}
		days = n
	}
	v, err := app.ReadRiskGate(repoDir, projDir, time.Now().UTC().AddDate(0, 0, -days))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// handleGateProposal applies or dismisses an open proposal and answers with
// the gate's new view.
func handleGateProposal(w http.ResponseWriter, r *http.Request, repoDir, projDir string) {
	if !localWrite(w, r) {
		return
	}
	var req struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Decision != app.ProposalAccepted && req.Decision != app.ProposalDismissed {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid request body: decision is "+app.ProposalAccepted+" or "+app.ProposalDismissed))
		return
	}
	if err := app.DecideGateProposal(repoDir, projDir, req.ID, req.Decision); err != nil {
		writeRiskError(w, err)
		return
	}
	settingsBroadcast()
	handleRiskGate(w, r, repoDir, projDir)
}

// writeRiskError answers a refused risk-gate write with the status its
// sentinel names.
func writeRiskError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, store.ErrNotFound), errors.Is(err, app.ErrUnknownProposal):
		status = http.StatusNotFound
	case errors.Is(err, app.ErrNoRiskDecision), errors.Is(err, app.ErrFeedbackMismatch), errors.Is(err, app.ErrFeatureFrozen):
		status = http.StatusConflict
	case errors.Is(err, app.ErrInvalidAcceptMode), errors.Is(err, domain.ErrUnknownFacet):
		status = http.StatusBadRequest
	default:
		writeConfigError(w, err) // a list item the settings refuse
		return
	}
	writeJSON(w, status, errorBody(err.Error()))
}
