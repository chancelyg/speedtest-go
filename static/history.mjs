// history.mjs — Test history drawer (Phase 2-3 agent F3).
//
// Owns the side panel that lists recent test results. Public API:
//
//   mountHistory(containerEl, opts)  -> instance
//   instance.refresh()
//   instance.setLang(lang)
//
// opts:
//   { apiBase: '/api/results', limit: 20, lang: 'zh' | 'en' }
//
// Design notes:
//   - All DOM access is guarded by `typeof document !== 'undefined'` so the
//     module can be imported by Node-based unit tests without throwing.
//   - We render via textContent / createElement only (no innerHTML), which
//     makes the table safe against XSS regardless of what client_ip or
//     bufferbloat_grade strings the backend stores.
//   - Errors during fetch are reflected as an inline error row; they never
//     propagate to the caller (mounted UI must not crash the speedtest UI).
//
// JSON contract: see internal/handler/results_handler.go (`Result` schema).

/* ── i18n strings ──────────────────────────────────────────────────────── */

const STRINGS = {
  zh: {
    title:        '历史记录',
    refresh:      '刷新',
    refreshing:   '加载中…',
    empty:        '还没有测速记录',
    error:        '加载历史记录失败',
    colTime:      '时间',
    colDownload:  '下载',
    colUpload:    '上传',
    colLatency:   '延迟',
    colGrade:     'Bufferbloat',
  },
  en: {
    title:        'History',
    refresh:      'Refresh',
    refreshing:   'Loading…',
    empty:        'No records yet',
    error:        'Failed to load history',
    colTime:      'Time',
    colDownload:  '↓ Mbps',
    colUpload:    '↑ Mbps',
    colLatency:   'Latency',
    colGrade:     'Bufferbloat',
  },
};

/**
 * Mount the history drawer onto a container element.
 * @param {HTMLElement} containerEl
 * @param {{ apiBase?: string, limit?: number, lang?: 'zh'|'en' }} [opts]
 * @returns {{ refresh(): Promise<void>, setLang(lang: string): void }}
 */
export function mountHistory(containerEl, opts = {}) {
  const apiBase = opts.apiBase || '/api/results';
  const limit   = Number.isFinite(opts.limit) ? Math.max(1, Math.min(100, opts.limit)) : 20;
  let lang      = STRINGS[opts.lang] ? opts.lang : 'zh';

  // Lightweight state — kept as references so refresh() can update body
  // contents without re-rendering the shell.
  const refs = {
    titleEl:    null,
    refreshBtn: null,
    bodyEl:     null,
  };

  function t(key) {
    return STRINGS[lang]?.[key] ?? key;
  }

  function renderShell() {
    if (typeof document === 'undefined' || !containerEl) return;
    // Clear existing children safely.
    while (containerEl.firstChild) containerEl.removeChild(containerEl.firstChild);
    containerEl.classList?.add('history-card');

    const header = document.createElement('header');
    header.className = 'history-head';

    const title = document.createElement('h2');
    title.className = 'history-title';
    title.textContent = t('title');
    refs.titleEl = title;

    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'history-refresh';
    btn.textContent = t('refresh');
    btn.addEventListener('click', () => { refresh().catch(() => {}); });
    refs.refreshBtn = btn;

    header.appendChild(title);
    header.appendChild(btn);
    containerEl.appendChild(header);

    const body = document.createElement('div');
    body.className = 'history-body';
    refs.bodyEl = body;
    containerEl.appendChild(body);

    renderEmpty();
  }

  function renderEmpty() {
    if (!refs.bodyEl) return;
    while (refs.bodyEl.firstChild) refs.bodyEl.removeChild(refs.bodyEl.firstChild);
    const div = document.createElement('div');
    div.className = 'history-empty';
    div.textContent = t('empty');
    refs.bodyEl.appendChild(div);
  }

  function renderError() {
    if (!refs.bodyEl) return;
    while (refs.bodyEl.firstChild) refs.bodyEl.removeChild(refs.bodyEl.firstChild);
    const div = document.createElement('div');
    div.className = 'history-empty history-error';
    div.textContent = t('error');
    refs.bodyEl.appendChild(div);
  }

  function renderRows(rows) {
    if (!refs.bodyEl) return;
    while (refs.bodyEl.firstChild) refs.bodyEl.removeChild(refs.bodyEl.firstChild);

    if (!rows || rows.length === 0) {
      renderEmpty();
      return;
    }

    const table = document.createElement('table');
    table.className = 'history-table';

    const thead = document.createElement('thead');
    const trh = document.createElement('tr');
    for (const key of ['colTime', 'colDownload', 'colUpload', 'colLatency', 'colGrade']) {
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

      const tdTime = document.createElement('td');
      tdTime.textContent = formatTimestamp(r.created_at);
      tr.appendChild(tdTime);

      const tdDl = document.createElement('td');
      tdDl.className = 'history-cell-num';
      tdDl.textContent = formatMbps(r.download_mbps);
      tr.appendChild(tdDl);

      const tdUl = document.createElement('td');
      tdUl.className = 'history-cell-num';
      tdUl.textContent = formatMbps(r.upload_mbps);
      tr.appendChild(tdUl);

      const tdLat = document.createElement('td');
      tdLat.className = 'history-cell-num';
      // Prefer loaded latency (under-load is what users care about); fall
      // back to idle if loaded wasn't measured.
      const lat = Number(r.latency_loaded_ms) > 0
        ? r.latency_loaded_ms
        : r.latency_idle_ms;
      tdLat.textContent = formatLatency(lat);
      tr.appendChild(tdLat);

      const tdGrade = document.createElement('td');
      tdGrade.className = 'history-cell-grade';
      tdGrade.textContent = sanitizeGrade(r.bufferbloat_grade);
      tr.appendChild(tdGrade);

      tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    refs.bodyEl.appendChild(table);
  }

  async function refresh() {
    if (typeof fetch !== 'function') {
      // Node/test environment — nothing to do, but conform to Promise<void>.
      return;
    }
    if (refs.refreshBtn) {
      refs.refreshBtn.disabled = true;
      refs.refreshBtn.textContent = t('refreshing');
    }
    try {
      const url = `${apiBase}?limit=${encodeURIComponent(limit)}&offset=0`;
      const r = await fetch(url, { cache: 'no-store' });
      if (!r.ok) {
        renderError();
        return;
      }
      const data = await r.json();
      const rows = Array.isArray(data?.results) ? data.results : [];
      renderRows(rows);
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
    // Re-render the shell + last-known body. Simpler to just refetch — the
    // history endpoint is cheap and this also picks up any newly persisted
    // run that landed since the last view.
    renderShell();
    refresh().catch(() => {});
  }

  renderShell();
  // Kick off an initial load.  Errors are absorbed by refresh()'s try/catch.
  refresh().catch(() => {});

  return { refresh, setLang };
}

/* ── formatting helpers ────────────────────────────────────────────────── */

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

function sanitizeGrade(g) {
  // Whitelist the grades the backend documents; anything else renders as
  // an em-dash so a corrupted row can't smuggle markup or surprising text
  // into the table.
  if (g === 'A' || g === 'B' || g === 'C' || g === 'D') return g;
  return '--';
}
