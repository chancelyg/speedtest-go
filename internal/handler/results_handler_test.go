package handler_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
	"speedtest-go/internal/store"
)

// resultsCfg is a minimal config for results-handler tests. The values are
// not relevant to history behaviour but New() needs a non-nil Config.
func resultsCfg() *config.Config {
	return &config.Config{
		Mode:          config.ModeTime,
		Duration:      5 * time.Second,
		MaxConcurrent: 4,
	}
}

// newHandlerWithStore returns a handler.Handler wired to a fresh on-disk
// SQLite store rooted in t.TempDir.
func newHandlerWithStore(t *testing.T) (*handler.Handler, store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return handler.New(resultsCfg(), s), s
}

// seed inserts n rows with strictly ascending CreatedAt timestamps and returns
// the saved Results (oldest first).
func seed(t *testing.T, s store.Store, n int) []store.Result {
	t.Helper()
	out := make([]store.Result, 0, n)
	base := int64(1_700_000_000_000)
	for i := 0; i < n; i++ {
		r, err := s.Save(context.Background(), store.Result{
			CreatedAt:    base + int64(i)*1000,
			DownloadMbps: float64(i + 1),
			UploadMbps:   float64(i+1) / 2,
			ClientIP:     "127.0.0.1",
			UserAgent:    "seed",
		})
		if err != nil {
			t.Fatalf("seed save: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// ── history disabled (nil store) ─────────────────────────────────────────────

func TestResultsAllReturn503WhenStoreNil(t *testing.T) {
	h := handler.New(resultsCfg(), nil)
	cases := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"list", http.MethodGet, "/api/results", h.ResultsListOrCreate},
		{"create", http.MethodPost, "/api/results", h.ResultsListOrCreate},
		{"export", http.MethodGet, "/api/results/export", h.ResultsExport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			tc.handler(w, req)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want 503", w.Code)
			}
			var body map[string]string
			_ = json.NewDecoder(w.Body).Decode(&body)
			if !strings.Contains(body["error"], "history disabled") {
				t.Errorf("error body = %q, want history disabled", body["error"])
			}
		})
	}
}

// ── POST /api/results ────────────────────────────────────────────────────────

func TestCreateResultStoresAndReturnsID(t *testing.T) {
	h, s := newHandlerWithStore(t)

	payload := `{"download_mbps":100.5,"upload_mbps":20.25,"latency_idle_ms":5,"bufferbloat_grade":"A"}`
	req := httptest.NewRequest(http.MethodPost, "/api/results", strings.NewReader(payload))
	req.Header.Set("User-Agent", "ua-test")
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["id"] == nil || body["created_at"] == nil {
		t.Errorf("missing id/created_at in response: %v", body)
	}

	// Server must have overwritten client_ip + user_agent.
	got, err := s.List(context.Background(), 1, 0)
	if err != nil || len(got) != 1 {
		t.Fatalf("List: got=%v err=%v", got, err)
	}
	if got[0].UserAgent != "ua-test" {
		t.Errorf("UserAgent = %q, want ua-test", got[0].UserAgent)
	}
	if got[0].DownloadMbps != 100.5 {
		t.Errorf("DownloadMbps = %v, want 100.5", got[0].DownloadMbps)
	}
}

func TestCreateResultRejectsInvalidJSON(t *testing.T) {
	h, _ := newHandlerWithStore(t)
	req := httptest.NewRequest(http.MethodPost, "/api/results", strings.NewReader("not-json"))
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateResultMethodFallback(t *testing.T) {
	// PUT is neither GET nor POST nor DELETE → 405.
	h, _ := newHandlerWithStore(t)
	req := httptest.NewRequest(http.MethodPut, "/api/results", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// ── GET /api/results ─────────────────────────────────────────────────────────

func TestListReturnsNewestFirst(t *testing.T) {
	h, s := newHandlerWithStore(t)
	seed(t, s, 3)

	req := httptest.NewRequest(http.MethodGet, "/api/results", nil)
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body struct {
		Results []store.Result `json:"results"`
		Total   int64          `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Total != 3 {
		t.Errorf("total = %d, want 3", body.Total)
	}
	if len(body.Results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(body.Results))
	}
	for i := 0; i < len(body.Results)-1; i++ {
		if body.Results[i].CreatedAt < body.Results[i+1].CreatedAt {
			t.Errorf("not newest-first at index %d", i)
		}
	}
}

func TestListLimitAndOffsetClamping(t *testing.T) {
	h, s := newHandlerWithStore(t)
	seed(t, s, 5)

	cases := []struct {
		query        string
		wantLen      int
	}{
		{"?limit=2", 2},
		{"?limit=0", 5},     // clamps to 1 — wait: 0 < 1 → limit=1; expect 1 row
		{"?limit=9999", 5},  // clamps to maxListLimit=100, but only 5 exist
		{"?offset=2", 3},
		{"?offset=9999999", 0},
	}
	// Re-evaluate "?limit=0": clampInt(0, 1, 100) = 1
	cases[1].wantLen = 1

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/results"+tc.query, nil)
			w := httptest.NewRecorder()
			h.ResultsListOrCreate(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			var body struct {
				Results []store.Result `json:"results"`
			}
			_ = json.NewDecoder(w.Body).Decode(&body)
			if len(body.Results) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(body.Results), tc.wantLen)
			}
		})
	}
}

// ── GET /api/results/export ─────────────────────────────────────────────────

func TestExportJSON(t *testing.T) {
	h, s := newHandlerWithStore(t)
	seed(t, s, 3)

	req := httptest.NewRequest(http.MethodGet, "/api/results/export?format=json", nil)
	w := httptest.NewRecorder()
	h.ResultsExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	cd := w.Result().Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".json") {
		t.Errorf("Content-Disposition = %q, missing attachment/.json", cd)
	}
	var body struct {
		Results []store.Result `json:"results"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Results) != 3 {
		t.Errorf("len = %d, want 3", len(body.Results))
	}
}

func TestExportCSV(t *testing.T) {
	h, s := newHandlerWithStore(t)
	seed(t, s, 2)

	req := httptest.NewRequest(http.MethodGet, "/api/results/export?format=csv", nil)
	w := httptest.NewRecorder()
	h.ResultsExport(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	cd := w.Result().Header.Get("Content-Disposition")
	if !strings.Contains(cd, ".csv") {
		t.Errorf("Content-Disposition = %q, missing .csv", cd)
	}

	rdr := csv.NewReader(bytes.NewReader(w.Body.Bytes()))
	rows, err := rdr.ReadAll()
	if err != nil {
		t.Fatalf("csv read: %v", err)
	}
	// 1 header + 2 data rows
	if len(rows) != 3 {
		t.Errorf("csv rows = %d, want 3 (incl header)", len(rows))
	}
	if rows[0][0] != "id" {
		t.Errorf("first header = %q, want id", rows[0][0])
	}
}

func TestExportInvalidFormat(t *testing.T) {
	h, _ := newHandlerWithStore(t)
	req := httptest.NewRequest(http.MethodGet, "/api/results/export?format=xml", nil)
	w := httptest.NewRecorder()
	h.ResultsExport(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestExportRejectsNonGET(t *testing.T) {
	h, _ := newHandlerWithStore(t)
	req := httptest.NewRequest(http.MethodPost, "/api/results/export", nil)
	w := httptest.NewRecorder()
	h.ResultsExport(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// ── DELETE rejected (DELETE endpoints were removed) ─────────────────────────

func TestResultsRejectsDelete(t *testing.T) {
	h, _ := newHandlerWithStore(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/results", nil)
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// ── /api/config historyEnabled flag ─────────────────────────────────────────

func TestConfigHandlerHistoryEnabled(t *testing.T) {
	cases := []struct {
		name        string
		store       store.Store
		wantEnabled bool
	}{
		{"nil store", nil, false},
	}
	// Add a "with store" case using a real on-disk store.
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "cfg.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	cases = append(cases, struct {
		name        string
		store       store.Store
		wantEnabled bool
	}{"with store", s, true})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := handler.New(resultsCfg(), tc.store)
			req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			w := httptest.NewRecorder()
			h.ConfigHandler(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			var body map[string]any
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body["historyEnabled"] != tc.wantEnabled {
				t.Errorf("historyEnabled = %v, want %v", body["historyEnabled"], tc.wantEnabled)
			}
		})
	}
}

// ── round-trip via httptest.Server (integration) ───────────────────────────

func TestResultsRoundTripViaHTTPServer(t *testing.T) {
	h, _ := newHandlerWithStore(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/results", h.ResultsListOrCreate)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// POST → 201.
	body := strings.NewReader(`{"download_mbps":42.0,"upload_mbps":12.0}`)
	resp, err := http.Post(srv.URL+"/api/results", "application/json", body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	// GET → 200, total=1.
	resp, err = http.Get(srv.URL + "/api/results")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	var listBody struct {
		Results []store.Result `json:"results"`
		Total   int64          `json:"total"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&listBody)
	resp.Body.Close()
	if listBody.Total != 1 {
		t.Errorf("total = %d, want 1", listBody.Total)
	}
}

// ── H-3: malicious POST body sanitisation ───────────────────────────────────

func TestCreateResultSanitisesHostileFields(t *testing.T) {
	h, s := newHandlerWithStore(t)

	body := `{
		"download_mbps": 1e20,
		"upload_mbps": -1,
		"latency_idle_ms": 1e20,
		"packet_loss": 200,
		"bufferbloat_grade": "<script>",
		"settings_json": "` + strings.Repeat("A", 5000) + `"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/results", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}

	rows, err := s.List(context.Background(), 1, 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("List: rows=%d err=%v", len(rows), err)
	}
	r := rows[0]
	if r.DownloadMbps != 1_000_000 {
		t.Errorf("DownloadMbps not clamped: got %v, want 1000000", r.DownloadMbps)
	}
	if r.UploadMbps != 0 {
		t.Errorf("UploadMbps negative not floored: got %v", r.UploadMbps)
	}
	if r.PacketLoss != 100 {
		t.Errorf("PacketLoss not clamped to [0,100]: got %v", r.PacketLoss)
	}
	if r.BufferbloatGrade != "" {
		t.Errorf("BufferbloatGrade hostile value not blanked: %q", r.BufferbloatGrade)
	}
	if len(r.SettingsJSON) > 1024 {
		t.Errorf("SettingsJSON not truncated: len=%d", len(r.SettingsJSON))
	}
}

func TestCreateResultRejectsNaN(t *testing.T) {
	h, s := newHandlerWithStore(t)
	// JSON spec doesn't allow bare NaN; the decoder errors out before we
	// reach sanitiseResult. Confirm we return 400 with the generic message.
	body := strings.NewReader(`{"download_mbps":NaN}`)
	req := httptest.NewRequest(http.MethodPost, "/api/results", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ResultsListOrCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	n, _ := s.Count(context.Background())
	if n != 0 {
		t.Errorf("malformed body should not have persisted: count=%d", n)
	}
}

// ── H-4: CSV injection prefix ───────────────────────────────────────────────

func TestExportCSVNeutralisesFormulaInjection(t *testing.T) {
	h, s := newHandlerWithStore(t)
	// Inject hostile client_ip / user_agent / settings_json via direct store
	// write (mirrors what a hostile POST would persist after sanitisation —
	// note sanitiseResult zeroes ClientIP/UserAgent before save, so the only
	// realistic vector is settings_json or a future field).
	_, err := s.Save(context.Background(), store.Result{
		BufferbloatGrade: "A",
		SettingsJSON:     `=cmd|'/C calc'!A0`,
	})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/results/export?format=csv", nil)
	w := httptest.NewRecorder()
	h.ResultsExport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	rows, err := csv.NewReader(w.Body).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	// Header + 1 data row.
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(rows))
	}
	settingsCol := rows[1][12]
	if !strings.HasPrefix(settingsCol, "'") {
		t.Errorf("formula not prefixed with apostrophe: %q", settingsCol)
	}
}
