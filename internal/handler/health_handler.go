package handler

import (
	"net/http"
	"time"
)

// healthResponse is the JSON shape returned by /healthz. Field names match
// the Prometheus-style snake_case convention so the same payload can be
// scraped by ops tooling.
//
// version / commit / date come from Handler.Build (ldflag-injected in main
// at release-build time). version is "dev" for local `go run`; commit /
// date may be empty when the build wasn't produced by goreleaser.
type healthResponse struct {
	Status         string `json:"status"`
	UptimeSec      int64  `json:"uptime_sec"`
	ActiveTests    int    `json:"active_tests"`
	AcceptedTotal  int64  `json:"accepted_total"`
	RejectedTotal  int64  `json:"rejected_total"`
	HistoryEnabled bool   `json:"history_enabled"`
	Version        string `json:"version"`
	Commit         string `json:"commit"`
	Date           string `json:"date"`
}

// HealthHandler serves GET /healthz and reports a tiny operational snapshot:
// the current uptime, how many concurrent tests are running, and the total
// number of tests accepted vs. rejected since the process started.
//
// The endpoint is read-only and rejects every non-GET method with 405 so it
// cannot be abused as a write surface.
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, healthResponse{
		Status:         "ok",
		UptimeSec:      int64(time.Since(h.startedAt).Seconds()),
		ActiveTests:    len(h.sem),
		AcceptedTotal:  h.acceptedTotal.Load(),
		RejectedTotal:  h.rejectedTotal.Load(),
		HistoryEnabled: h.historyEnabled(),
		Version:        h.versionOrDev(),
		Commit:         h.commitOrEmpty(),
		Date:           h.dateOrEmpty(),
	})
}
