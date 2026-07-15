package handler

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"speedtest-go/internal/store"
)

// genericInternalError is the body returned to clients on 500 responses. We
// intentionally do not surface err.Error() because driver-level messages can
// contain schema names, file paths or OS-level details that aid attackers
// fingerprinting the deployment. The full error is logged server-side instead
// (loggingMiddleware in main.go already correlates by X-Request-Id).
const genericInternalError = "internal error"

// internalErr logs the full err at error level and writes the generic message
// to the client. Correlate via the X-Request-Id header in the access log.
func internalErr(w http.ResponseWriter, r *http.Request, op string, err error) {
	slog.Error("handler internal error",
		"op", op,
		"path", r.URL.Path,
		"err", err.Error(),
	)
	writeErr(w, http.StatusInternalServerError, genericInternalError)
}

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

// ResultsListOrCreate is the multiplex handler bound to GET /api/results and
// POST /api/results. The two methods share a path because the underlying
// resource ("the results collection") is the same; routing inside the handler
// keeps main.go's mux setup compact.
//
// DELETE methods are intentionally not exposed: the frontend has no UI for
// destructive operations, and adding them would require a real authn story
// (the previous loopback-only check is bypassed by any reverse proxy on the
// same host, which is the typical deployment topology).
func (h *Handler) ResultsListOrCreate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listResults(w, r)
	case http.MethodPost:
		h.createResult(w, r)
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
		internalErr(w, r, "store.List", err)
		return
	}
	total, err := h.store.Count(r.Context())
	if err != nil {
		internalErr(w, r, "store.Count", err)
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
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in store.Result
	if err := dec.Decode(&in); err != nil {
		// Don't echo the raw decoder message — it contains field types /
		// byte offsets that leak internal struct shape. Generic + log only.
		slog.Warn("results decode", "path", r.URL.Path, "err", err.Error())
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// H-3: sanitise / clamp client-supplied numeric and string fields BEFORE
	// the server-side overwrites below, so a malicious client can't poison
	// the row with NaN/Inf, oversized text or fake grades.
	sanitiseResult(&in)

	// Server-controlled fields. Always overwrite — clients cannot lie about
	// timestamp, peer IP, or user agent. UA is truncated to keep row size
	// bounded (some bots send multi-kB UAs).
	in.CreatedAt = time.Now().UnixMilli()
	in.ClientIP = ClientIP(r)
	in.UserAgent = truncate(r.Header.Get("User-Agent"), 512)
	// Enrich with geoip when the operator opted in. The resolver returns ""
	// for private/loopback IPs and mmdb misses, so we don't need to guard
	// the ip parse or the ClientIPLocation write beyond the nil-handle check.
	// Stored at write time so historical rows keep their snapshot even if
	// the mmdb is later swapped or removed.
	// LocateIP holds the RLock across the lookup so a concurrent
	// SetGeoIP(new) can't Close (munmap) the reader mid-decode.
	in.ClientIPLocation = h.LocateIP(net.ParseIP(in.ClientIP))

	saved, err := h.store.Save(r.Context(), in)
	if err != nil {
		internalErr(w, r, "store.Save", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":         saved.ID,
		"created_at": saved.CreatedAt,
	})
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
		internalErr(w, r, "store.List/export", err)
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

// writeResultsCSV writes a CSV header + one row per result. String fields
// pass through csvSafe to neutralise formula-injection payloads (=, +, -, @,
// CR, TAB prefixes) that Excel / LibreOffice would otherwise interpret as
// executable formulae when the file is opened.
func writeResultsCSV(w http.ResponseWriter, results []store.Result) {
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"id", "created_at", "download_mbps", "upload_mbps",
		"latency_idle_ms", "latency_loaded_ms",
		"download_jitter_ms", "upload_jitter_ms",
		"packet_loss", "bufferbloat_grade",
		"client_ip", "client_ip_location", "user_agent", "settings_json",
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
			csvSafe(r.BufferbloatGrade),
			csvSafe(r.ClientIP),
			csvSafe(r.ClientIPLocation),
			csvSafe(r.UserAgent),
			csvSafe(r.SettingsJSON),
		})
	}
}

// csvSafe prefixes values that would be interpreted as a formula by Excel /
// LibreOffice with a single quote. The leading apostrophe is invisible in
// the spreadsheet cell but neutralises the OLE-style command execution.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// sanitiseResult clamps client-supplied numeric fields to safe ranges,
// rejects non-finite floats, normalises BufferbloatGrade to the documented
// enum, and bounds SettingsJSON length. Called from createResult before the
// row is handed to the store.
func sanitiseResult(r *store.Result) {
	// Floats: NaN/Inf become 0; mbps capped at 1e6 (1 Tbps — far above any
	// real link), latency/jitter capped at 60_000 ms, packet loss at 100.
	r.DownloadMbps = clampFloat(r.DownloadMbps, 0, 1_000_000)
	r.UploadMbps = clampFloat(r.UploadMbps, 0, 1_000_000)
	r.LatencyIdleMs = clampFloat(r.LatencyIdleMs, 0, 60_000)
	r.LatencyLoadedMs = clampFloat(r.LatencyLoadedMs, 0, 60_000)
	r.DownloadJitterMs = clampFloat(r.DownloadJitterMs, 0, 60_000)
	r.UploadJitterMs = clampFloat(r.UploadJitterMs, 0, 60_000)
	r.PacketLoss = clampFloat(r.PacketLoss, 0, 100)

	// Bufferbloat grade: enum of "" / A / B / C / D. Anything else becomes "".
	switch r.BufferbloatGrade {
	case "", "A", "B", "C", "D":
		// pass
	default:
		r.BufferbloatGrade = ""
	}

	// SettingsJSON: bound size; clients only ever send a tiny object like
	// {"mode":"time","duration":15,"streams":4}. Reject anything north of
	// 1 kB as a safety net against row bloat.
	r.SettingsJSON = truncate(r.SettingsJSON, 1024)

	// Clients should never set ID/CreatedAt/ClientIP/ClientIPLocation/UserAgent
	// — the server overwrites those after this function returns. Zero them
	// out to avoid any accidental persistence of a forged value. Zeroing
	// ClientIPLocation specifically prevents a client from fabricating a
	// location string when the server has geoip disabled (in which case the
	// enrichment step in createResult is a no-op and would otherwise leave
	// the client-supplied value in place).
	r.ID = 0
	r.CreatedAt = 0
	r.ClientIP = ""
	r.ClientIPLocation = ""
	r.UserAgent = ""
}

// clampFloat returns v constrained to [lo, hi]. NaN and ±Inf return lo.
func clampFloat(v, lo, hi float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// truncate caps s at n bytes. Multibyte-safe in the sense that it cuts on a
// UTF-8 boundary by trimming any trailing run of continuation bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	// Walk back to a UTF-8 boundary (a byte whose top bits are not 10xxxxxx).
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut]
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
