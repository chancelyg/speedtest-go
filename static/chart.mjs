// chart.mjs — Speed-over-time SVG line chart (Phase 2-3 agent F2).
//
// Owns: rendering primitives for the real-time speed-over-time graph that
// fills <svg id="speed-chart"> during an active test. Should expose a small
// public API used by app.js:
//
//   renderChart(svgEl, opts)    -> instance
//   instance.pushPoint(t, mbpsDl, mbpsUl)
//   instance.reset()
//
// Implementation must NOT pull in Chart.js / D3 / any external module
// (single-binary, no-build constraints). Use plain SVG <polyline>.
//
// Reuse the same gradient CSS variables as the gauges in style.css for
// visual consistency. Honour prefers-reduced-motion (no transition pulses).
//
// Skeleton placeholder so app.js can import it during predispatch without
// failing — the real implementation is committed by agent F2.

export function renderChart(svgEl, opts = {}) {
  void svgEl; void opts;
  return {
    pushPoint(_t, _dl, _ul) { /* F2 to implement */ },
    reset() { /* F2 to implement */ },
  };
}
