package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"speedtest-go/internal/store"
)

// historyDisabledMsg is returned in the JSON envelope when the store is nil.
// Keeping the wording stable allows the frontend to test for the exact string.
const historyDisabledMsg = "history disabled"

// maxListLimit caps `?limit=` on /api/results to keep response sizes bounded.
const maxListLimit = 100

// maxListOffset caps `?offset=` so a malicious crawler cannot force the
// database to compute arbitrarily deep pages.
const maxListOffset = 1_000_000

// errResponse is the standard envelope for non-2xx JSON replies.
type errResponse struct {
	Error string `json:"error"`
}

// writeErr serialises a JSON error body with the given status code.
func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errResponse{Error: msg})
}

// writeHistoryDisabled responds 503 + JSON when h.store is nil.
func writeHistoryDisabled(w http.ResponseWriter) {
	writeErr(w, http.StatusServiceUnavailable, historyDisabledMsg)
}

// isLoopbackPeer returns true when the request's direct peer address is a
// loopback IP (127.0.0.0/8 or ::1). Destructive endpoints use this to limit
// access to the same host even when the server is bound on 0.0.0.0. We use
// the *direct* RemoteAddr (not ClientIP) on purpose: ClientIP trusts proxy
// headers from private peers, but admin endpoints must NOT trust headers.
func isLoopbackPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

// ResultsListOrCreate is the multiplex handler bound to GET /api/results and
// POST /api/results. The two methods share a path because the underlying
// resource ("the results collection") is the same; routing inside the handler
// keeps main.go's mux setup compact.
func (h *Handler) ResultsListOrCreate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listResults(w, r)
	case http.MethodPost:
		h.createResult(w, r)
	case http.MethodDelete:
		h.deleteAllResults(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// listResults serves GET /api/results?limit=&offset=.
func (h *Handler) listResults(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeHistoryDisabled(w)
		return
	}
	limit := clampInt(parseQueryInt(r, "limit", 20), 1, maxListLimit)
	offset := clampInt(parseQueryInt(r, "offset", 0), 0, maxListOffset)

	results, err := h.store.List(r.Context(), limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	total, err := h.store.Count(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{
		"results": results,
		"total":   total,
		"limit":   limit,
		"offset":  offset,
	})
}

// createResult serves POST /api/results.
//
// The handler trusts the JSON body for measurement fields but always
// overwrites ClientIP and UserAgent from the request so a client cannot lie
// about either. CreatedAt is also server-assigned for the same reason.
func (h *Handler) createResult(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeHistoryDisabled(w)
		return
	}
	// 1 MB body cap is enormous for a result document (typical < 1 kB) but
	// cheap to enforce and stops a malformed-JSON denial of service.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in store.Result
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	in.CreatedAt = time.Now().UnixMilli()
	in.ClientIP = ClientIP(r)
	in.UserAgent = r.Header.Get("User-Agent")

	saved, err := h.store.Save(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         saved.ID,
		"created_at": saved.CreatedAt,
	})
}

// deleteAllResults serves DELETE /api/results. Loopback-gated.
func (h *Handler) deleteAllResults(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeHistoryDisabled(w)
		return
	}
	if !isLoopbackPeer(r) {
		writeErr(w, http.StatusForbidden, "loopback only")
		return
	}
	n, err := h.store.DeleteAll(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"deleted": n})
}

// ResultsByID handles requests at /api/results/{id}. Currently only DELETE is
// supported (loopback-gated). GET-by-id is not part of the P2.5/P3.1 surface
// so we 404 it explicitly to avoid leaking implementation details.
func (h *Handler) ResultsByID(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeHistoryDisabled(w)
		return
	}
	// Disambiguate /api/results/range and /api/results/export, which share
	// the /api/results/ prefix in the mux. These have dedicated handlers and
	// should never reach here when routing is correct — but defensive coding
	// keeps the surface stable if the mux pattern set changes.
	rest := strings.TrimPrefix(r.URL.Path, "/api/results/")
	switch rest {
	case "", "export":
		http.NotFound(w, r)
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackPeer(r) {
		writeErr(w, http.StatusForbidden, "loopback only")
		return
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ok, err := h.store.Delete(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, map[string]any{"deleted": id})
}

// ResultsExport serves GET /api/results/export?format=csv|json.
//
// Always exports the full table. Content-Disposition header carries a
// UTC-timestamped filename so curl/browser downloads land with a useful name.
func (h *Handler) ResultsExport(w http.ResponseWriter, r *http.Request) {
	if h.store == nil {
		writeHistoryDisabled(w)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeErr(w, http.StatusBadRequest, "format must be json or csv")
		return
	}

	// Cap at 100k rows: an enormous local history but still bounded. Single-
	// machine deployment is the target audience; nobody is exporting > 100k
	// speed tests through the browser.
	results, err := h.store.List(r.Context(), 100_000, 0)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	ts := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("speedtest-results-%s.%s", ts, format)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	switch format {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		writeResultsCSV(w, results)
	}
}

// writeResultsCSV writes a CSV header + one row per result.
func writeResultsCSV(w http.ResponseWriter, results []store.Result) {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"id", "created_at", "download_mbps", "upload_mbps",
		"latency_idle_ms", "latency_loaded_ms",
		"download_jitter_ms", "upload_jitter_ms",
		"packet_loss", "bufferbloat_grade",
		"client_ip", "user_agent", "settings_json",
	})
	for _, r := range results {
		_ = cw.Write([]string{
			strconv.FormatInt(r.ID, 10),
			strconv.FormatInt(r.CreatedAt, 10),
			strconv.FormatFloat(r.DownloadMbps, 'f', -1, 64),
			strconv.FormatFloat(r.UploadMbps, 'f', -1, 64),
			strconv.FormatFloat(r.LatencyIdleMs, 'f', -1, 64),
			strconv.FormatFloat(r.LatencyLoadedMs, 'f', -1, 64),
			strconv.FormatFloat(r.DownloadJitterMs, 'f', -1, 64),
			strconv.FormatFloat(r.UploadJitterMs, 'f', -1, 64),
			strconv.FormatFloat(r.PacketLoss, 'f', -1, 64),
			r.BufferbloatGrade,
			r.ClientIP,
			r.UserAgent,
			r.SettingsJSON,
		})
	}
}

// parseQueryInt parses an int from r.URL.Query()[key], returning def when
// absent or unparseable. Used for limit/offset.
func parseQueryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// clampInt returns n clamped to [lo, hi].
func clampInt(n, lo, hi int) int {
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}
