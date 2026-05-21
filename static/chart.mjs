// chart.mjs — Speed-over-time SVG line chart (Phase 2-3 agent F2).
//
// Renders a real-time download/upload throughput chart into <svg id="speed-
// chart"> during an active test. Pure SVG <polyline>, no external libs.
//
// Public API
// ──────────
//   renderChart(svgEl, opts) → { pushPoint(t, dlMbps, ulMbps), reset() }
//
// opts:
//   maxPoints  default 600 (60 s @ 100 ms cadence)
//   maxTimeMs  default 60_000
//
// pushPoint is rAF-throttled: many calls per frame coalesce into one redraw.
// `null` for a series means "no sample this tick" → that point is skipped
// when drawing that series' polyline (the other series still renders).
//
// Design notes
// ────────────
// • Log10 Y axis (0.1 → 1000 Mbps) matches the semicircle gauge scale.
// • Pure helpers (pushBounded, pointsString, logScaleY) are exported for
//   unit tests so we don't need a DOM in node --test.
// • Honours `prefers-reduced-motion: reduce` — no polyline transitions.

/* ── Constants (exported for tests) ──────────────────────────────────────── */

export const CHART_W = 600;
export const CHART_H = 200;

// Log axis range. 0.1 Mbps is the visible floor, 1000 Mbps the ceiling.
// log10(0.1) = -1, log10(1000) = 3 → span = 4 decades.
const LOG_MIN = -1;
const LOG_MAX = 3;
const LOG_SPAN = LOG_MAX - LOG_MIN;

const DEFAULT_MAX_POINTS  = 600;
const DEFAULT_MAX_TIME_MS = 60_000;

const SVG_NS = 'http://www.w3.org/2000/svg';

/* ── Pure helpers ────────────────────────────────────────────────────────── */

/**
 * Append `item` to `arr` immutably; if length would exceed `maxLen`, drop
 * the oldest entry. Returns a NEW array (never mutates input).
 *
 * @template T
 * @param {ReadonlyArray<T>} arr
 * @param {T} item
 * @param {number} maxLen
 * @returns {T[]}
 */
export function pushBounded(arr, item, maxLen) {
  const next = arr.slice();
  next.push(item);
  while (next.length > maxLen) next.shift();
  return next;
}

/**
 * Map a Mbps value to a Y pixel coordinate in the chart's viewBox space.
 * Higher Mbps → smaller Y (closer to the top). Clamps to [0, CHART_H].
 *
 * @param {number} mbps
 * @returns {number}
 */
export function logScaleY(mbps) {
  if (!Number.isFinite(mbps) || mbps <= 0) return CHART_H;
  const l = Math.log10(mbps);
  if (l <= LOG_MIN) return CHART_H;
  if (l >= LOG_MAX) return 0;
  const frac = (l - LOG_MIN) / LOG_SPAN;
  return CHART_H - frac * CHART_H;
}

/**
 * Map a timestamp (ms since test start) to an X coordinate. Linear scale.
 * Clamps to [0, CHART_W].
 *
 * @param {number} t
 * @param {number} maxTimeMs
 * @returns {number}
 */
function timeScaleX(t, maxTimeMs) {
  if (!Number.isFinite(t) || t <= 0) return 0;
  if (t >= maxTimeMs) return CHART_W;
  return (t / maxTimeMs) * CHART_W;
}

/**
 * Build the SVG `points` attribute for one series. Skips entries where v is
 * null (renders as a gap in the polyline). Returns "" if no usable points.
 *
 * @param {ReadonlyArray<{ t: number, v: number | null }>} points
 * @param {number} maxTimeMs
 * @returns {string}
 */
export function pointsString(points, maxTimeMs) {
  const parts = [];
  for (const p of points) {
    if (p.v === null || p.v === undefined) continue;
    const x = timeScaleX(p.t, maxTimeMs);
    const y = logScaleY(p.v);
    parts.push(x.toFixed(2).replace(/\.?0+$/, '') + ',' + y.toFixed(2).replace(/\.?0+$/, ''));
  }
  return parts.join(' ');
}

/* ── DOM helpers ─────────────────────────────────────────────────────────── */

function el(svg, tagName, attrs) {
  const doc = svg.ownerDocument || (typeof document !== 'undefined' ? document : null);
  const node = doc
    ? doc.createElementNS(SVG_NS, tagName)
    : { tagName, attrs: {}, setAttribute(k, v) { this.attrs[k] = v; }, appendChild() {} };
  if (attrs) {
    for (const k in attrs) node.setAttribute(k, attrs[k]);
  }
  return node;
}

/**
 * Mount the static chart structure (grid lines, axis tick labels, polylines)
 * into svgEl. Returns the two polyline nodes so updates can hot-patch them.
 */
function mountStructure(svgEl) {
  // Clear any previous children so calling renderChart twice is safe.
  while (svgEl.firstChild) svgEl.removeChild(svgEl.firstChild);

  // Background grid — 3 horizontal lines (matches log decades 1/10/100 Mbps).
  const gridGroup = el(svgEl, 'g', { class: 'chart-grid' });
  const decadeMbps = [1, 10, 100];
  for (const v of decadeMbps) {
    const y = logScaleY(v);
    gridGroup.appendChild(el(svgEl, 'line', {
      x1: '0', x2: String(CHART_W),
      y1: String(y), y2: String(y),
      class: 'chart-grid-line',
    }));
    // Tick label
    const txt = el(svgEl, 'text', {
      x: '4', y: String(y - 2),
      class: 'chart-tick-label',
    });
    txt.textContent = v + ' Mbps';
    gridGroup.appendChild(txt);
  }

  // 4 vertical guides (25/50/75 % of the time axis).
  for (let i = 1; i <= 3; i++) {
    const x = (CHART_W * i) / 4;
    gridGroup.appendChild(el(svgEl, 'line', {
      x1: String(x), x2: String(x),
      y1: '0', y2: String(CHART_H),
      class: 'chart-grid-line chart-grid-line-v',
    }));
  }
  svgEl.appendChild(gridGroup);

  // Polylines — order matters: upload behind, download in front so the
  // "primary" measurement is visually on top.
  const upline = el(svgEl, 'polyline', {
    class: 'chart-line chart-line-ul',
    fill: 'none',
    points: '',
  });
  const dlline = el(svgEl, 'polyline', {
    class: 'chart-line chart-line-dl',
    fill: 'none',
    points: '',
  });
  svgEl.appendChild(upline);
  svgEl.appendChild(dlline);

  return { dlline, upline };
}

/* ── Public API ──────────────────────────────────────────────────────────── */

/**
 * Mount a real-time speed chart onto an existing <svg> element.
 *
 * @param {SVGElement} svgEl
 * @param {{ maxPoints?: number, maxTimeMs?: number }} [opts]
 * @returns {{
 *   pushPoint: (elapsedMs: number, dlMbps: number | null, ulMbps: number | null) => void,
 *   reset: () => void,
 *   _peek?: () => { dl: any[], ul: any[] },
 * }}
 */
export function renderChart(svgEl, opts = {}) {
  const maxPoints  = opts.maxPoints  || DEFAULT_MAX_POINTS;
  const maxTimeMs  = opts.maxTimeMs  || DEFAULT_MAX_TIME_MS;

  // Two independent point arrays. Splitting per series means a null value
  // for one direction doesn't pollute the other's polyline.
  let dl = [];
  let ul = [];

  const { dlline, upline } = mountStructure(svgEl);

  // rAF throttle — same pattern as setSpeed() in app.js, so multiple
  // pushPoint() calls within one frame coalesce into a single DOM write.
  let rafScheduled = false;
  let dirty = false;

  // Use a global handle so tests that stub requestAnimationFrame still work.
  const raf = (typeof requestAnimationFrame === 'function')
    ? requestAnimationFrame
    : (cb) => setTimeout(() => cb(Date.now()), 16);

  function flush() {
    rafScheduled = false;
    if (!dirty) return;
    dirty = false;
    dlline.setAttribute('points', pointsString(dl, maxTimeMs));
    upline.setAttribute('points', pointsString(ul, maxTimeMs));
  }

  function schedule() {
    dirty = true;
    if (rafScheduled) return;
    rafScheduled = true;
    raf(flush);
  }

  function pushPoint(elapsedMs, dlMbps, ulMbps) {
    if (!Number.isFinite(elapsedMs)) return;
    dl = pushBounded(dl, { t: elapsedMs, v: (dlMbps == null ? null : dlMbps) }, maxPoints);
    ul = pushBounded(ul, { t: elapsedMs, v: (ulMbps == null ? null : ulMbps) }, maxPoints);
    schedule();
  }

  function reset() {
    dl = [];
    ul = [];
    dirty = true;
    // Flush synchronously on reset so the chart visibly clears before the
    // next test starts; otherwise the previous run's lines linger until the
    // first pushPoint of the new run triggers a redraw.
    flush();
  }

  return {
    pushPoint,
    reset,
    // Test-only inspector — not part of the public contract.
    _peek() { return { dl: dl.slice(), ul: ul.slice() }; },
  };
}
