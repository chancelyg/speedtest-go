package handler

// Results endpoints skeleton — to be implemented by Phase 2-3 agent B1.
//
// All endpoints below are gated on cfg.DBPath != "" (otherwise return 404 or
// a feature-disabled JSON response — agent B1 to decide; see /api/config
// "history_enabled" boolean that the frontend will read).
//
// Endpoints:
//
//   POST /api/results
//     Body: JSON Result (see Result schema below)
//     Returns: {"id": int, "created_at": int64} on success
//
//   GET /api/results?limit=20&offset=0
//     Returns: {"results": [Result, ...], "total": int}
//
//   GET /api/results/range?from=<unixMs>&to=<unixMs>
//     Returns: {"results": [Result, ...]}  // ordered by created_at asc
//
//   GET /api/results/export?format=csv|json&from=<unixMs>&to=<unixMs>
//     Returns: file download with Content-Disposition: attachment;
//             filename="speedtest-history-<YYYYMMDD>.csv|json"
//
//   DELETE /api/results            -> wipe all (require loopback peer)
//   DELETE /api/results/{id}       -> delete one (require loopback peer)
//
// Result JSON schema (shared with frontend F3):
//
//	{
//	  "id":                    int,        // omitted on POST
//	  "created_at":            int64,      // unix ms; server fills if absent
//	  "download_mbps":         float,
//	  "upload_mbps":           float,
//	  "latency_idle_ms":       float,
//	  "latency_loaded_ms":     float,      // optional, 0 if missing
//	  "download_jitter_ms":    float,
//	  "upload_jitter_ms":      float,
//	  "packet_loss":           float,      // percent 0..100
//	  "bufferbloat_grade":     string,     // "A" | "B" | "C" | "D" | ""
//	  "client_ip":             string,     // server fills via ClientIP(r)
//	  "user_agent":            string,     // server fills from r.UserAgent()
//	  "settings_json":         string      // raw JSON: {"mode","duration","streams"}
//	}
//
// Implementation notes:
//   - Use internal/store package (separate sibling) for persistence.
//   - DELETE endpoints must use ClientIP loopback gating to prevent remote wipe.
//   - Loopback admin gating helper lives at handler.isLoopbackPeer (B1 to add).
