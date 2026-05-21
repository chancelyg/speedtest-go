// trends.mjs — Historical trend chart (Phase 2-3 agent F3).
//
// Owns the multi-line time-series chart showing download/upload/latency over
// the last 24h / 7d / 30d. Public API:
//
//   mountTrends(containerEl, opts)  -> instance
//   instance.setWindow('24h' | '7d' | '30d')
//   instance.refresh()
//   instance.exportCSV() / exportJSON()  -> triggers download via /api/results/export
//
// opts:
//   { apiBase: '/api/results', lang: 'zh'|'en' }
//
// Aggregates raw points into per-bucket medians client-side (no backend
// aggregation). Uses the same plain-SVG polyline approach as chart.mjs
// (no Chart.js / D3). Hover tooltip displays absolute timestamp + 3 values.
//
// Skeleton placeholder so app.js can import it during predispatch without
// failing — the real implementation is committed by agent F3.

export function mountTrends(containerEl, opts = {}) {
  void containerEl; void opts;
  return {
    setWindow(_w) { /* F3 to implement */ },
    refresh() { /* F3 to implement */ },
    exportCSV() { /* F3 to implement */ },
    exportJSON() { /* F3 to implement */ },
  };
}
