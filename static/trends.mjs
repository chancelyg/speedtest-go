// trends.mjs — Historical trend chart (Phase 2-3 agent F3).
//
// Owns the time-series chart showing download / upload / latency over the
// last 24h / 7d / 30d, with CSV/JSON export buttons. Public API:
//
//   mountTrends(containerEl, opts)  -> instance
//   instance.setWindow('24h' | '7d' | '30d')   -> Promise<void>
//   instance.refresh()                          -> Promise<void>
//   instance.setLang(lang)                      -> void
//
// opts:
//   { apiBase: '/api/results', lang: 'zh' | 'en' }
//
// Design notes:
//   - Charts are vanilla SVG polylines. No Chart.js / D3 — the only
//     dependency is the browser's native rendering.
//   - Raw points come from /api/results/range, then we aggregate
//     client-side via bucketAndMedian (exported below for unit tests).
//   - Bucket sizes are tuned per window so that no chart ever has fewer
//     than ~6 points (visually empty) or more than ~50 (visually noisy):
//       24h → 30min buckets   → up to 48 points
//        7d → 6h   buckets    → up to 28 points
//       30d → 1d   buckets    → up to 30 points
//     This is the rationale for promoting bucketAndMedian to a pure,
//     individually testable export — bucket-size tuning is the most
//     opinionated bit of this module and the easiest to get wrong.

/* ── Window definitions ────────────────────────────────────────────────── */

const WINDOWS = {
  '24h': { spanMs: 24 * 3600_000,           bucketMs: 30 * 60_000   },
  '7d':  { spanMs: 7  * 24 * 3600_000,      bucketMs: 6  * 3600_000 },
  '30d': { spanMs: 30 * 24 * 3600_000,      bucketMs: 24 * 3600_000 },
};

const STRINGS = {
  zh: {
    title:      '趋势',
    win24h:     '24 小时',
    win7d:      '7 天',
    win30d:     '30 天',
    exportCsv:  '导出 CSV',
    exportJson: '导出 JSON',
    speedTitle: '速度 (Mbps)',
    latTitle:   '延迟 (ms)',
    download:   '下载',
    upload:     '上传',
    latency:    '延迟',
    empty:      '该时间段还没有数据',
    error:      '加载趋势数据失败',
  },
  en: {
    title:      'Trends',
    win24h:     '24h',
    win7d:      '7d',
    win30d:     '30d',
    exportCsv:  'Export CSV',
    exportJson: 'Export JSON',
    speedTitle: 'Speed (Mbps)',
    latTitle:   'Latency (ms)',
    download:   'Download',
    upload:     'Upload',
    latency:    'Latency',
    empty:      'No data in this window',
    error:      'Failed to load trends',
  },
};

/* ── Pure aggregator (exported for unit tests) ─────────────────────────── */

/**
 * Bucket a list of points into fixed-width time buckets and return the
 * median value per bucket.
 *
 * @param {Array<{t: number, v: number}>} points  Time-ascending or unordered.
 * @param {number} bucketMs                       Bucket width in ms (>0).
 * @returns {Array<{t: number, v: number}>}       One entry per non-empty bucket,
 *                                                ordered by bucket start.
 */
export function bucketAndMedian(points, bucketMs) {
  if (!Array.isArray(points) || points.length === 0) return [];
  if (!Number.isFinite(bucketMs) || bucketMs <= 0)   return [];

  /** @type {Map<number, number[]>} */
  const buckets = new Map();
  for (const p of points) {
    const t = Number(p?.t);
    const v = Number(p?.v);
    if (!Number.isFinite(t) || !Number.isFinite(v)) continue;
    const key = Math.floor(t / bucketMs) * bucketMs;
    let arr = buckets.get(key);
    if (!arr) { arr = []; buckets.set(key, arr); }
    arr.push(v);
  }

  const keys = [...buckets.keys()].sort((a, b) => a - b);
  return keys.map(k => ({ t: k, v: median(buckets.get(k)) }));
}

function median(values) {
  const sorted = values.slice().sort((a, b) => a - b);
  const n = sorted.length;
  if (n === 0) return 0;
  if (n % 2 === 1) return sorted[(n - 1) / 2];
  return (sorted[n / 2 - 1] + sorted[n / 2]) / 2;
}

/* ── mountTrends ───────────────────────────────────────────────────────── */

/**
 * Mount the trends panel onto a container element.
 * @param {HTMLElement} containerEl
 * @param {{ apiBase?: string, lang?: 'zh'|'en' }} [opts]
 * @returns {{
 *   setWindow(w: '24h'|'7d'|'30d'): Promise<void>,
 *   refresh(): Promise<void>,
 *   setLang(lang: string): void,
 * }}
 */
export function mountTrends(containerEl, opts = {}) {
  const apiBase = opts.apiBase || '/api/results';
  let lang      = STRINGS[opts.lang] ? opts.lang : 'zh';
  let currentWindow = '24h';

  const refs = {
    titleEl:    null,
    winButtons: null,        // Map<windowKey, HTMLElement>
    speedSvg:   null,
    latSvg:     null,
    statusEl:   null,
    exportCsvBtn:  null,
    exportJsonBtn: null,
  };

  function t(key) { return STRINGS[lang]?.[key] ?? key; }

  function renderShell() {
    if (typeof document === 'undefined' || !containerEl) return;
    while (containerEl.firstChild) containerEl.removeChild(containerEl.firstChild);
    containerEl.classList?.add('trends-card');

    // ── Header: title + window toggle + export buttons
    const header = document.createElement('header');
    header.className = 'trends-head';

    const title = document.createElement('h2');
    title.className = 'trends-title';
    title.textContent = t('title');
    refs.titleEl = title;
    header.appendChild(title);

    const winToggle = document.createElement('div');
    winToggle.className = 'trends-window-toggle';
    winToggle.setAttribute('role', 'tablist');
    refs.winButtons = new Map();
    for (const key of ['24h', '7d', '30d']) {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'trends-window-btn';
      b.dataset.window = key;
      // STRINGS uses lowercase keys (win24h / win7d / win30d) so we don't
      // uppercase here — toUpperCase would yield "win24H" which has no match
      // and the raw key would leak into the UI.
      b.textContent = t(`win${key}`);
      if (key === currentWindow) b.classList.add('active');
      b.addEventListener('click', () => { setWindow(key).catch(() => {}); });
      winToggle.appendChild(b);
      refs.winButtons.set(key, b);
    }
    header.appendChild(winToggle);

    const exports = document.createElement('div');
    exports.className = 'trends-export';
    const csvBtn = document.createElement('button');
    csvBtn.type = 'button';
    csvBtn.className = 'trends-export-btn';
    csvBtn.textContent = t('exportCsv');
    csvBtn.addEventListener('click', () => triggerExport('csv'));
    refs.exportCsvBtn = csvBtn;
    exports.appendChild(csvBtn);

    const jsonBtn = document.createElement('button');
    jsonBtn.type = 'button';
    jsonBtn.className = 'trends-export-btn';
    jsonBtn.textContent = t('exportJson');
    jsonBtn.addEventListener('click', () => triggerExport('json'));
    refs.exportJsonBtn = jsonBtn;
    exports.appendChild(jsonBtn);

    header.appendChild(exports);
    containerEl.appendChild(header);

    // ── Charts
    const speedChart = makeChartBlock(t('speedTitle'));
    refs.speedSvg = speedChart.svg;
    containerEl.appendChild(speedChart.wrap);

    const latChart = makeChartBlock(t('latTitle'));
    refs.latSvg = latChart.svg;
    containerEl.appendChild(latChart.wrap);

    // ── Status (empty / error) line
    const status = document.createElement('div');
    status.className = 'trends-status';
    refs.statusEl = status;
    containerEl.appendChild(status);
  }

  function makeChartBlock(label) {
    const wrap = document.createElement('div');
    wrap.className = 'trends-chart-block';

    const cap = document.createElement('div');
    cap.className = 'trends-chart-caption';
    cap.textContent = label;
    wrap.appendChild(cap);

    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('class', 'trends-chart');
    svg.setAttribute('viewBox', '0 0 600 200');
    svg.setAttribute('preserveAspectRatio', 'none');
    svg.setAttribute('aria-hidden', 'true');
    wrap.appendChild(svg);
    return { wrap, svg };
  }

  function setStatus(msg) {
    if (!refs.statusEl) return;
    refs.statusEl.textContent = msg || '';
    refs.statusEl.hidden = !msg;
  }

  function currentRange() {
    const now = Date.now();
    const span = WINDOWS[currentWindow].spanMs;
    return { from: now - span, to: now };
  }

  function triggerExport(format) {
    if (typeof window === 'undefined') return;
    const { from, to } = currentRange();
    const url = `${apiBase}/export?format=${encodeURIComponent(format)}&from=${from}&to=${to}`;
    // Plain <a download> click → browser handles the file save with the
    // Content-Disposition the server sets. We don't open a new tab because
    // some browsers block downloads from window.open.
    const a = document.createElement('a');
    a.href = url;
    a.rel = 'noopener';
    // Filename hint — server's Content-Disposition wins if set.
    a.download = '';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  async function refresh() {
    if (typeof fetch !== 'function') return;
    setStatus('');
    const { from, to } = currentRange();
    let results;
    try {
      const url = `${apiBase}/range?from=${from}&to=${to}`;
      const r = await fetch(url, { cache: 'no-store' });
      if (!r.ok) { setStatus(t('error')); clearCharts(); return; }
      const data = await r.json();
      results = Array.isArray(data?.results) ? data.results : [];
    } catch (_err) {
      setStatus(t('error'));
      clearCharts();
      return;
    }
    if (results.length === 0) {
      setStatus(t('empty'));
      clearCharts();
      return;
    }
    drawCharts(results);
  }

  function clearCharts() {
    [refs.speedSvg, refs.latSvg].forEach(svg => {
      if (!svg) return;
      while (svg.firstChild) svg.removeChild(svg.firstChild);
    });
  }

  function drawCharts(results) {
    const bucketMs = WINDOWS[currentWindow].bucketMs;

    const dlPts = bucketAndMedian(
      results.map(r => ({ t: Number(r.created_at), v: Number(r.download_mbps) })),
      bucketMs,
    );
    const ulPts = bucketAndMedian(
      results.map(r => ({ t: Number(r.created_at), v: Number(r.upload_mbps) })),
      bucketMs,
    );
    const latPts = bucketAndMedian(
      results
        .map(r => {
          const loaded = Number(r.latency_loaded_ms);
          const idle   = Number(r.latency_idle_ms);
          const v = Number.isFinite(loaded) && loaded > 0 ? loaded : idle;
          return { t: Number(r.created_at), v };
        }),
      bucketMs,
    );

    const { from, to } = currentRange();

    drawMultiLine(refs.speedSvg, [
      { points: dlPts, cssClass: 'trends-line-dl', label: t('download') },
      { points: ulPts, cssClass: 'trends-line-ul', label: t('upload')   },
    ], { from, to });

    drawMultiLine(refs.latSvg, [
      { points: latPts, cssClass: 'trends-line-lat', label: t('latency') },
    ], { from, to });
  }

  async function setWindow(w) {
    if (!WINDOWS[w]) return;
    currentWindow = w;
    if (refs.winButtons) {
      for (const [key, btn] of refs.winButtons) {
        btn.classList.toggle('active', key === w);
      }
    }
    await refresh();
  }

  function setLang(next) {
    if (!STRINGS[next] || next === lang) return;
    lang = next;
    renderShell();
    refresh().catch(() => {});
  }

  renderShell();
  refresh().catch(() => {});

  return { setWindow, refresh, setLang };
}

/* ── SVG line drawing ──────────────────────────────────────────────────── */

const VIEW_W = 600;
const VIEW_H = 200;
const PAD    = 8;          // small inner padding so endpoints don't clip

function drawMultiLine(svg, series, range) {
  if (!svg) return;
  while (svg.firstChild) svg.removeChild(svg.firstChild);

  // Compute global y bounds from all non-empty series.
  let yMin = Infinity, yMax = -Infinity;
  for (const s of series) {
    for (const p of s.points) {
      if (p.v < yMin) yMin = p.v;
      if (p.v > yMax) yMax = p.v;
    }
  }
  if (!Number.isFinite(yMin) || !Number.isFinite(yMax)) return;
  if (yMin === yMax) { yMin -= 1; yMax += 1; }
  // Pad y range by ~10% so the line doesn't kiss the chart edge.
  const span = yMax - yMin;
  yMin -= span * 0.08;
  yMax += span * 0.08;

  const xMin = range.from;
  const xMax = range.to;
  const xSpan = xMax - xMin || 1;

  const sx = t => PAD + ((t - xMin) / xSpan) * (VIEW_W - 2 * PAD);
  const sy = v => VIEW_H - PAD - ((v - yMin) / (yMax - yMin)) * (VIEW_H - 2 * PAD);

  // Baseline (subtle horizontal rule at the bottom).
  const ns = 'http://www.w3.org/2000/svg';
  const axis = document.createElementNS(ns, 'line');
  axis.setAttribute('x1', String(PAD));
  axis.setAttribute('x2', String(VIEW_W - PAD));
  axis.setAttribute('y1', String(VIEW_H - PAD));
  axis.setAttribute('y2', String(VIEW_H - PAD));
  axis.setAttribute('class', 'trends-axis');
  svg.appendChild(axis);

  for (const s of series) {
    if (!s.points || s.points.length === 0) continue;
    const coords = s.points
      .map(p => `${sx(p.t).toFixed(1)},${sy(p.v).toFixed(1)}`)
      .join(' ');
    const polyline = document.createElementNS(ns, 'polyline');
    polyline.setAttribute('points', coords);
    polyline.setAttribute('class', `trends-line ${s.cssClass || ''}`.trim());
    polyline.setAttribute('fill', 'none');
    polyline.setAttribute('vector-effect', 'non-scaling-stroke');
    svg.appendChild(polyline);

    // Render small dots at each sample so single-point series remain visible.
    for (const p of s.points) {
      const dot = document.createElementNS(ns, 'circle');
      dot.setAttribute('cx', sx(p.t).toFixed(1));
      dot.setAttribute('cy', sy(p.v).toFixed(1));
      dot.setAttribute('r', '2');
      dot.setAttribute('class', `trends-dot ${s.cssClass || ''}`.trim());
      svg.appendChild(dot);
    }
  }
}
