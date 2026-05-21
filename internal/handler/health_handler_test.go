package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"speedtest-go/internal/config"
	"speedtest-go/internal/handler"
)

func healthCfg() *config.Config {
	return &config.Config{
		Mode:          config.ModeTime,
		Duration:      5 * time.Second,
		MaxConcurrent: 4,
	}
}

func TestHealthHandlerReturnsOK(t *testing.T) {
	h := handler.New(healthCfg(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.HealthHandler(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
	if _, ok := body["uptime_sec"]; !ok {
		t.Error("missing uptime_sec field")
	}
	if v, _ := body["active_tests"].(float64); v != 0 {
		t.Errorf("active_tests = %v, want 0", v)
	}
	if body["history_enabled"] != false {
		t.Errorf("history_enabled = %v, want false", body["history_enabled"])
	}
}

func TestHealthHandlerRejectsNonGET(t *testing.T) {
	h := handler.New(healthCfg(), nil)
	for _, method := range []string{
		http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/healthz", nil)
			w := httptest.NewRecorder()
			h.HealthHandler(w, req)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", w.Code)
			}
		})
	}
}

func TestHealthHandlerNoCachingHeaders(t *testing.T) {
	h := handler.New(healthCfg(), nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.HealthHandler(w, req)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
}
