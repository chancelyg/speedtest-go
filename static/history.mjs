// history.mjs — Test history drawer (Phase 2-3 agent F3).
//
// Owns the side drawer / panel that lists recent test results. Public API:
//
//   mountHistory(containerEl, opts)  -> instance
//   instance.refresh()
//   instance.show() / hide() / toggle()
//
// opts:
//   { apiBase: '/api/results', limit: 20, lang: 'zh'|'en' }
//
// During development F3 may stub the fetch with a `mockResults()` to avoid
// a hard dependency on B1's backend; once B1 lands, switch to the real
// /api/results endpoint (see internal/handler/results_handler.go for the
// JSON contract).
//
// Skeleton placeholder so app.js can import it during predispatch without
// failing — the real implementation is committed by agent F3.

export function mountHistory(containerEl, opts = {}) {
  void containerEl; void opts;
  return {
    refresh() { /* F3 to implement */ },
    show() { /* F3 to implement */ },
    hide() { /* F3 to implement */ },
    toggle() { /* F3 to implement */ },
  };
}
