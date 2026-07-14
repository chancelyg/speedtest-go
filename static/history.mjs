// history.mjs — Paginated test history with inline CSV/JSON export buttons.
//
// Single-machine deployment focus: the history card is the only data view.
// Renders {pageSize} rows per page with classic ‹prev / numbered / next›
// pagination and a header strip with refresh + CSV + JSON buttons.
//
// Public API:
//
//   mountHistory(containerEl, opts) -> {
//     refresh(): Promise<void>,        // reload current page
//     setLang(lang: 'zh'|'en'): void,  // switch labels in place
//     goToPage(n: number): Promise<void>,
//   }
//
// opts: { apiBase?: string, pageSize?: number, lang?: 'zh'|'en' }
//
// JSON contract: see internal/handler/results_handler.go (`Result` schema).
// Pagination is server-driven via /api/results?limit=&offset=; the response
// envelope `{results, total, limit, offset}` is what drives page math.

/* ── i18n strings ──────────────────────────────────────────────────────── */

const STRINGS = {
  zh: {
    title:       '历史记录',
    refresh:     '刷新',
    refreshing:  '加载中…',
    exportCsv:   '导出 CSV',
    exportJson:  '导出 JSON',
    empty:       '还没有测速记录',
    error:       '加载历史记录失败',
    colTime:     '时间',
    colIp:       '来源 IP',
    colDownload: '下载',
    colUpload:   '上传',
    colLatency:  '延迟',
    colGrade:    'Bufferbloat',
    prev:        '上一页',
    next:        '下一页',
    pageInfo:    (total, from, to) => `共 ${total} 条 · ${from}–${to}`,
  },
  en: {
    title:       'History',
    refresh:     'Refresh',
    refreshing:  'Loading…',
    exportCsv:   'Export CSV',
    exportJson:  'Export JSON',
    empty:       'No records yet',
    error:       'Failed to load history',
    colTime:     'Time',
    colIp:       'Source IP',
    colDownload: '↓ Mbps',
    colUpload:   '↑ Mbps',
    colLatency:  'Latency',
    colGrade:    'Bufferbloat',
    prev:        'Prev',
    next:        'Next',
    pageInfo:    (total, from, to) => `${total} total · ${from}–${to}`,
  },
};

/* ── Pure pagination helper (exported for tests) ───────────────────────── */

/**
 * Compute the page-button window for a numbered pager:
 *
 *   « prev   1 … 4 5 [6] 7 8 … 20   next »
 *
 * Returns an array of entries describing what to render. Each entry is either
 * an integer page number (1-based) or the sentinel string '…' for an
 * ellipsis. Page 1 and the last page are always included so the user can
 * jump to either end in one click.
 *
 * @param {number} totalPages - 1-based total page count, must be >= 1.
 * @param {number} currentPage - 1-based current page, clamped to [1, totalPages].
 * @param {number} [windowSize=2] - pages to show on each side of currentPage.
 * @returns {Array<number | '…'>}
 */
export function computePageWindow(totalPages, currentPage, windowSize = 2) {
  const total = Math.max(1, Math.floor(Number(totalPages) || 1));
  const cur   = Math.min(total, Math.max(1, Math.floor(Number(currentPage) || 1)));
  const w     = Math.max(0, Math.floor(Number(windowSize) || 0));

  // Smaller than ~7 pages: list them all, no ellipsis needed.
  if (total <= 7) {
    return Array.from({ length: total }, (_, i) => i + 1);
  }

  const out = [1];
  const left  = Math.max(2, cur - w);
  const right = Math.min(total - 1, cur + w);

  if (left > 2) out.push('…');
  for (let p = left; p <= right; p++) out.push(p);
  if (right < total - 1) out.push('…');
  out.push(total);
  return out;
}

/* ── mountHistory ──────────────────────────────────────────────────────── */

/**
 * @param {HTMLElement} containerEl
 * @param {{ apiBase?: string, pageSize?: number, lang?: 'zh'|'en' }} [opts]
 */
export function mountHistory(containerEl, opts = {}) {
  const apiBase  = opts.apiBase || '/api/results';
  const pageSize = clampInt(opts.pageSize, 5, 100, 20);
  let lang       = STRINGS[opts.lang] ? opts.lang : 'zh';

  // Mutable state.
  let currentPage = 1;
  let totalRows   = 0;

  const refs = {
    titleEl:      null,
    refreshBtn:   null,
    exportCsvBtn: null,
    exportJsonBtn:null,
    bodyEl:       null,
    pagerEl:      null,
  };

  const t = (k) => STRINGS[lang]?.[k] ?? k;

  function renderShell() {
    if (typeof document === 'undefined' || !containerEl) return;
    while (containerEl.firstChild) containerEl.removeChild(containerEl.firstChild);
    containerEl.classList?.add('history-card');

    // ── Header: title (left) · export-csv · export-json · refresh
    const header = document.createElement('header');
    header.className = 'history-head';

    const title = document.createElement('h2');
    title.className = 'history-title';
    title.textContent = t('title');
    refs.titleEl = title;
    header.appendChild(title);

    const exports = document.createElement('div');
    exports.className = 'history-export';

    refs.exportCsvBtn = makeExportBtn('csv');
    refs.exportJsonBtn = makeExportBtn('json');
    exports.appendChild(refs.exportCsvBtn);
    exports.appendChild(refs.exportJsonBtn);
    header.appendChild(exports);

    const refreshBtn = document.createElement('button');
    refreshBtn.type = 'button';
    refreshBtn.className = 'history-refresh';
    refreshBtn.textContent = t('refresh');
    refreshBtn.addEventListener('click', () => { refresh().catch(() => {}); });
    refs.refreshBtn = refreshBtn;
    header.appendChild(refreshBtn);

    containerEl.appendChild(header);

    const body = document.createElement('div');
    body.className = 'history-body';
    refs.bodyEl = body;
    containerEl.appendChild(body);

    const pager = document.createElement('div');
    pager.className = 'history-pager';
    pager.hidden = true;
    refs.pagerEl = pager;
    containerEl.appendChild(pager);

    renderEmpty();
  }

  function makeExportBtn(format) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'history-export-btn';
    b.dataset.format = format;
    b.textContent = t(format === 'csv' ? 'exportCsv' : 'exportJson');
    b.addEventListener('click', () => triggerExport(format));
    return b;
  }

  function triggerExport(format) {
    if (typeof window === 'undefined') return;
    // /api/results/export always returns the full table; the backend sets
    // Content-Disposition so the browser saves to disk with a useful name.
    const url = `${apiBase}/export?format=${encodeURIComponent(format)}`;
    const a = document.createElement('a');
    a.href = url;
    a.rel = 'noopener';
    a.target = '_self';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  function renderEmpty() {
    if (!refs.bodyEl) return;
    clearChildren(refs.bodyEl);
    const div = document.createElement('div');
    div.className = 'history-empty';
    div.textContent = t('empty');
    refs.bodyEl.appendChild(div);
    if (refs.pagerEl) refs.pagerEl.hidden = true;
  }

  function renderError() {
    if (!refs.bodyEl) return;
    clearChildren(refs.bodyEl);
    const div = document.createElement('div');
    div.className = 'history-empty history-error';
    div.textContent = t('error');
    refs.bodyEl.appendChild(div);
    if (refs.pagerEl) refs.pagerEl.hidden = true;
  }

  function renderRows(rows) {
    if (!refs.bodyEl) return;
    clearChildren(refs.bodyEl);

    if (!rows || rows.length === 0) {
      renderEmpty();
      return;
    }

    const table = document.createElement('table');
    table.className = 'history-table';

    const thead = document.createElement('thead');
    const trh = document.createElement('tr');
    for (const key of ['colTime', 'colIp', 'colDownload', 'colUpload', 'colLatency', 'colGrade']) {
      const th = document.createElement('th');
      th.textContent = t(key);
      trh.appendChild(th);
    }
    thead.appendChild(trh);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const r of rows) {
      const tr = document.createElement('tr');
      tr.className = 'history-row';

      appendCell(tr, formatTimestamp(r.created_at));
      const ip = formatIp(r.client_ip);
      appendCell(tr, ip, 'history-cell-ip', ip === '--' ? '' : ip);
      appendCell(tr, formatMbps(r.download_mbps), 'history-cell-num');
      appendCell(tr, formatMbps(r.upload_mbps), 'history-cell-num');
      const lat = Number(r.latency_loaded_ms) > 0
        ? r.latency_loaded_ms
        : r.latency_idle_ms;
      appendCell(tr, formatLatency(lat), 'history-cell-num');
      appendCell(tr, sanitizeGrade(r.bufferbloat_grade), 'history-cell-grade');

      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    refs.bodyEl.appendChild(table);
  }

  function renderPager() {
    if (!refs.pagerEl) return;
    clearChildren(refs.pagerEl);

    const totalPages = Math.max(1, Math.ceil(totalRows / pageSize));
    if (totalRows <= pageSize) {
      refs.pagerEl.hidden = true;
      return;
    }
    refs.pagerEl.hidden = false;

    const fromRow = (currentPage - 1) * pageSize + 1;
    const toRow   = Math.min(totalRows, currentPage * pageSize);

    const info = document.createElement('span');
    info.className = 'history-pager-info';
    info.textContent = STRINGS[lang].pageInfo(totalRows, fromRow, toRow);
    refs.pagerEl.appendChild(info);

    // ‹ prev
    refs.pagerEl.appendChild(makePagerBtn(t('prev'), currentPage > 1, () => goToPage(currentPage - 1)));

    // numbered window
    for (const entry of computePageWindow(totalPages, currentPage)) {
      if (entry === '…') {
        const ell = document.createElement('span');
        ell.className = 'history-pager-ellipsis';
        ell.textContent = '…';
        refs.pagerEl.appendChild(ell);
      } else {
        const b = makePagerBtn(String(entry), entry !== currentPage, () => goToPage(entry));
        if (entry === currentPage) b.classList.add('active');
        refs.pagerEl.appendChild(b);
      }
    }

    // next ›
    refs.pagerEl.appendChild(makePagerBtn(t('next'), currentPage < totalPages, () => goToPage(currentPage + 1)));
  }

  function makePagerBtn(label, enabled, onClick) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'history-page-btn';
    b.textContent = label;
    if (!enabled) b.disabled = true;
    else b.addEventListener('click', onClick);
    return b;
  }

  async function goToPage(n) {
    const totalPages = Math.max(1, Math.ceil(totalRows / pageSize));
    const target = Math.min(totalPages, Math.max(1, Math.floor(n)));
    if (target === currentPage) return;
    currentPage = target;
    await refresh();
  }

  async function refresh() {
    if (typeof fetch !== 'function') return;
    if (refs.refreshBtn) {
      refs.refreshBtn.disabled = true;
      refs.refreshBtn.textContent = t('refreshing');
    }
    try {
      const offset = (currentPage - 1) * pageSize;
      const url = `${apiBase}?limit=${pageSize}&offset=${offset}`;
      const r = await fetch(url, { cache: 'no-store' });
      if (!r.ok) {
        renderError();
        return;
      }
      const data = await r.json();
      totalRows = Number(data?.total) || 0;

      // If a delete trimmed the table while we were on a high page, recenter.
      const totalPages = Math.max(1, Math.ceil(totalRows / pageSize));
      if (currentPage > totalPages) {
        currentPage = totalPages;
        return refresh();
      }

      const rows = Array.isArray(data?.results) ? data.results : [];
      renderRows(rows);
      renderPager();
    } catch (_err) {
      renderError();
    } finally {
      if (refs.refreshBtn) {
        refs.refreshBtn.disabled = false;
        refs.refreshBtn.textContent = t('refresh');
      }
    }
  }

  function setLang(next) {
    if (!STRINGS[next] || next === lang) return;
    lang = next;
    renderShell();
    refresh().catch(() => {});
  }

  renderShell();
  refresh().catch(() => {});

  return { refresh, setLang, goToPage };
}

/* ── tiny helpers ──────────────────────────────────────────────────────── */

function clearChildren(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
}

function appendCell(tr, text, cls, titleText) {
  const td = document.createElement('td');
  if (cls) td.className = cls;
  td.textContent = text;
  if (titleText) td.title = titleText;
  tr.appendChild(td);
}

function clampInt(v, min, max, def) {
  const n = Number(v);
  if (!Number.isFinite(n)) return def;
  return Math.min(max, Math.max(min, Math.floor(n)));
}

function formatTimestamp(ms) {
  const n = Number(ms);
  if (!Number.isFinite(n) || n <= 0) return '--';
  const d = new Date(n);
  const yyyy = d.getFullYear();
  const mm = String(d.getMonth() + 1).padStart(2, '0');
  const dd = String(d.getDate()).padStart(2, '0');
  const hh = String(d.getHours()).padStart(2, '0');
  const mi = String(d.getMinutes()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd} ${hh}:${mi}`;
}

function formatMbps(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '--';
  if (n >= 100)  return n.toFixed(0);
  if (n >= 10)   return n.toFixed(1);
  return n.toFixed(2);
}

function formatLatency(v) {
  const n = Number(v);
  if (!Number.isFinite(n) || n <= 0) return '--';
  return `${n.toFixed(1)} ms`;
}

function formatIp(v) {
  if (typeof v !== 'string') return '--';
  const s = v.trim();
  return s === '' ? '--' : s;
}

function sanitizeGrade(g) {
  if (g === 'A' || g === 'B' || g === 'C' || g === 'D') return g;
  return '--';
}
