package handler

// Health endpoint skeleton — to be implemented by Phase 2-3 agent B1.
//
// Target: GET /healthz returns JSON:
//
//	{
//	  "status":          "ok",
//	  "uptime_sec":      12345,
//	  "active_tests":    int,   // currently-running download/upload tests
//	  "accepted_total":  int,   // cumulative tests admitted (sem acquire ok)
//	  "rejected_total":  int    // cumulative tests rejected with 503
//	}
//
// The active_tests counter comes from len(h.sem); accepted/rejected counters
// must be added to Handler as atomic int64 fields and incremented in the
// existing acquire()/DownloadHandler/UploadHandler 503 paths (handler.go).
