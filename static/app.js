'use strict';

import { gaugeAngle, windowStats, pushWindow, throughputMbps, jitterRFC3550 } from './metrics.mjs';
import { mountToast } from './toast.mjs';
import { mountTooltips, refreshTooltips } from './tooltips.mjs';

import { mountHistory } from './history.mjs'; // F3: paginated history drawer

/* ── i18n ────────────────────────────────────────────────────────────────── */
const i18n = {
  zh: {
    title:        '网络测速',
    download:     '下载速度',
    upload:       '上传速度',
    latency:      '延迟',
    jitter:       '抖动',
    packetLoss:   '丢包率',
    ip:           'IP 地址',
    connection:   '连接方式',
    start:        '开始测速',
    stop:         '停止',
    stagePing:    '测量延迟…',
    stageDown:    '测量下载…',
    stageUp:      '测量上传…',
    done:         '重新测速',
    footer:       'Speedtest · Powered by Go',
    langBtn:      'English',
    mode:         '测速模式',
    modeTime:     '按时长',
    modeSize:     '按大小',
    duration:     '持续时间',
    downloadSize: '下载大小',
    uploadSize:   '上传大小',
    streams:      '并发流',
    sec:          '秒',
    mb:           'MB',
    cfgHint:      '点击展开测试设置',
    cfgHintOpen:  '收起测试设置',
    serverBusy:   '服务器繁忙，请稍后',
    networkError: '网络中断',
    unknownError: '未知错误',
    retry:        '重试',
    bufferbloat:  'Bufferbloat',
    hintLatency:     '一次请求往返时间，越低越好。显示的是整段测试期间的平均值。',
    hintJitter:      '延迟波动幅度，越低越稳定。采用 RFC 3550 平滑算法计算。',
    hintLoss:        '基于 HTTP 请求失败率，并非 UDP 丢包率。',
    hintBufferbloat: '负载下延迟比空闲时上升的程度。A 最好、F 最差。',
    hintIP:          '服务器看到的客户端公网 IP，反向代理后会显示代理识别的真实地址。',
    hintConn:        '浏览器上报的网络类型，仅作参考（部分浏览器不提供）。',
  },
  en: {
    title:        'Speedtest',
    download:     'Download',
    upload:       'Upload',
    latency:      'Latency',
    jitter:       'Jitter',
    packetLoss:   'Packet Loss',
    ip:           'IP Address',
    connection:   'Connected via',
    start:        'Start Test',
    stop:         'Stop',
    stagePing:    'Measuring latency…',
    stageDown:    'Measuring download…',
    stageUp:      'Measuring upload…',
    done:         'Test Again',
    footer:       'Speedtest · Powered by Go',
    langBtn:      '中文',
    mode:         'Mode',
    modeTime:     'Time',
    modeSize:     'Size',
    duration:     'Duration',
    downloadSize: 'Download Size',
    uploadSize:   'Upload Size',
    streams:      'Streams',
    sec:          's',
    mb:           'MB',
    cfgHint:      'Click to expand test settings',
    cfgHintOpen:  'Collapse test settings',
    serverBusy:   'Server busy, please retry',
    networkError: 'Network error',
    unknownError: 'Unknown error',
    retry:        'Retry',
    bufferbloat:  'Bufferbloat',
    hintLatency:     'Round-trip time per request, lower is better. Averaged across the whole test phase.',
    hintJitter:      'Variation in latency, lower is steadier. Computed with the RFC 3550 smoothing formula.',
    hintLoss:        'Based on HTTP request failure rate, not UDP packet loss.',
    hintBufferbloat: 'How much latency rises under load vs. idle. A is best, F is worst.',
    hintIP:          'Client public IP as seen by the server; behind a reverse proxy this is the forwarded address.',
    hintConn:        'Network type reported by the browser. Indicative only — not all browsers expose it.',
  },
};

// Persist language preference across page loads.
let lang = (() => {
  try { return localStorage.getItem('speedtest_lang') || 'zh'; } catch { return 'zh'; }
})();
let testing = false;

// Server config (loaded once from /api/config)
let srvCfg = { mode: 'time', durationSecs: 15, downloadMB: 25, uploadMB: 10, streams: 4 };

// Active config: localStorage > server config > defaults
let activeCfg = { ...srvCfg };

const LS_KEY = 'speedtest_config';

function loadLocalConfig() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (raw) return JSON.parse(raw);
  } catch { /* ignore */ }
  return null;
}

function saveLocalConfig(cfg) {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify(cfg));
  } catch { /* ignore */ }
}

// Clamp the merged numeric config to ranges the server is known to accept.
// localStorage is attacker-writable (devtools / extensions / shared machine),
// so this is defence-in-depth even though the server clamps again.
function clampNum(v, lo, hi, def) {
  const n = Number(v);
  if (!Number.isFinite(n)) return def;
  return Math.min(hi, Math.max(lo, n));
}

function mergeConfig(serverCfg, localCfg) {
  const defaults = { mode: 'time', durationSecs: 15, downloadMB: 25, uploadMB: 10, streams: 4 };
  const mode = (localCfg?.mode === 'size' || localCfg?.mode === 'time')
    ? localCfg.mode
    : (serverCfg.mode === 'size' || serverCfg.mode === 'time' ? serverCfg.mode : defaults.mode);
  return {
    mode,
    durationSecs: clampNum(localCfg?.durationSecs ?? serverCfg.durationSecs, 1, 300,   defaults.durationSecs),
    downloadMB:   clampNum(localCfg?.downloadMB   ?? serverCfg.downloadMB,   1, 10240, defaults.downloadMB),
    uploadMB:     clampNum(localCfg?.uploadMB     ?? serverCfg.uploadMB,     1, 10240, defaults.uploadMB),
    streams:      clampNum(localCfg?.streams      ?? serverCfg.streams,      1, 32,    defaults.streams),
  };
}

function initConfigUI() {
  const modeToggle = $('mode-toggle');
  const timeConfig = $('time-config');
  const sizeConfig = $('size-config');

  // Mode toggle
  modeToggle.addEventListener('click', e => {
    const btn = e.target.closest('.toggle-btn');
    if (!btn) return;
    modeToggle.querySelectorAll('.toggle-btn').forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    const mode = btn.dataset.value;
    activeCfg.mode = mode;
    if (mode === 'time') {
      timeConfig.style.display = 'flex';
      sizeConfig.style.display = 'none';
    } else {
      timeConfig.style.display = 'none';
      sizeConfig.style.display = 'flex';
    }
    saveLocalConfig(activeCfg);
  });

  // Selects
  const bindSelect = (id, key) => {
    const el = $(id);
    el.addEventListener('change', () => {
      activeCfg[key] = Number(el.value);
      saveLocalConfig(activeCfg);
    });
  };
  bindSelect('duration-select', 'durationSecs');
  bindSelect('download-size-select', 'downloadMB');
  bindSelect('upload-size-select', 'uploadMB');
  bindSelect('streams-select', 'streams');

  // Config panel collapse/expand
  const toggleBtn  = $('config-toggle');
  const panel      = $('config-panel');
  toggleBtn.addEventListener('click', () => {
    const expanded = toggleBtn.getAttribute('aria-expanded') === 'true';
    toggleBtn.setAttribute('aria-expanded', String(!expanded));
    panel.hidden = expanded;
    $('config-toggle-text').textContent = t(expanded ? 'cfgHint' : 'cfgHintOpen');
  });
}

function applyConfigToUI(cfg) {
  const modeBtn = $(cfg.mode === 'time' ? 'mode-time' : 'mode-size');
  modeBtn.click();

  $('duration-select').value      = String(cfg.durationSecs);
  $('download-size-select').value = String(cfg.downloadMB);
  $('upload-size-select').value   = String(cfg.uploadMB);
  $('streams-select').value       = String(cfg.streams);
}

const t = key => i18n[lang][key] ?? key;
const $ = id  => document.getElementById(id);

/* ── Toast (error feedback) ──────────────────────────────────────────────── */
// `showToast` is initialised on DOMContentLoaded (container must exist).
// Until then calls fall back to console.warn so early bootstrap errors are
// not swallowed.
let showToast = (msg, level) => console.warn(`[toast:${level}] ${msg}`);

// Classify a failed fetch into a toast message. Used at every API call site
// so the user always knows why a measurement didn't run. We deliberately
// avoid showing toasts for user-initiated aborts (the Stop button).
function toastForFetchError(err, status) {
  if (err && err.name === 'AbortError') return null;       // user-cancelled
  if (status === 503)                   return { msg: t('serverBusy'), level: 'warn' };
  if (status && status >= 500)          return { msg: t('unknownError'), level: 'error' };
  // TypeError from fetch() === network layer failure (DNS, offline, TLS, …).
  if (err instanceof TypeError)         return { msg: t('networkError'), level: 'error' };
  return { msg: t('unknownError'), level: 'error' };
}

/* ── Theme ───────────────────────────────────────────────────────────────── */
(function initTheme() {
  const mq  = window.matchMedia('(prefers-color-scheme: dark)');
  const btn = $('theme-toggle');
  const LS_THEME_KEY = 'speedtest_theme';

  const apply = dark => {
    document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light');
    btn.textContent = dark ? '☀️' : '🌙';
  };

  const saved = localStorage.getItem(LS_THEME_KEY);
  if (saved !== null) {
    apply(saved === 'dark');
  } else {
    apply(mq.matches);
  }

  mq.addEventListener('change', e => {
    if (localStorage.getItem(LS_THEME_KEY) === null) apply(e.matches);
  });

  btn.addEventListener('click', () => {
    const dark = document.documentElement.getAttribute('data-theme') !== 'dark';
    apply(dark);
    localStorage.setItem(LS_THEME_KEY, dark ? 'dark' : 'light');
  });
})();

/* ── Language ────────────────────────────────────────────────────────────── */
function applyLang() {
  $('title').textContent             = t('title');
  $('download-label').textContent    = t('download');
  $('upload-label').textContent      = t('upload');
  $('download-latency-label').textContent = t('latency');
  $('upload-latency-label').textContent   = t('latency');
  $('download-jitter-label').textContent  = t('jitter');
  $('upload-jitter-label').textContent    = t('jitter');
  $('packet-loss-label').textContent      = t('packetLoss');
  // === [F1: bufferbloat label i18n] ===
  const bbLabel = $('bufferbloat-label');
  if (bbLabel) bbLabel.textContent = t('bufferbloat');
  // === [F1 end] ===
  $('ip-label').textContent          = t('ip');
  $('connection-label').textContent  = t('connection');
  // Refresh hint glyph tooltips from the new language strings. Safe
  // before mountTooltips() — only the data attribute is touched.
  refreshTooltips(t);
  $('footer-text').textContent       = t('footer');
  $('lang-toggle').textContent       = t('langBtn');
  $('stop-text').textContent         = t('stop');
  if (!testing) $('start-text').textContent = t('start');

  // Config panel labels
  $('mode-label').textContent          = t('mode');
  $('mode-time').textContent           = t('modeTime');
  $('mode-size').textContent           = t('modeSize');
  $('duration-label').textContent      = t('duration');
  $('download-size-label').textContent = t('downloadSize');
  $('upload-size-label').textContent   = t('uploadSize');
  $('streams-label').textContent       = t('streams');

  // Config toggle hint text
  const expanded = $('config-toggle').getAttribute('aria-expanded') === 'true';
  $('config-toggle-text').textContent = t(expanded ? 'cfgHintOpen' : 'cfgHint');

  // Update select option labels
  updateSelectLabels('duration-select', 'sec');
  updateSelectLabels('download-size-select', 'mb');
  updateSelectLabels('upload-size-select', 'mb');
}

function updateSelectLabels(selectId, unitKey) {
  const select = $(selectId);
  const unit = t(unitKey);
  Array.from(select.options).forEach(opt => {
    opt.textContent = `${opt.value} ${unit}`;
  });
}

$('lang-toggle').addEventListener('click', () => {
  lang = lang === 'zh' ? 'en' : 'zh';
  try { localStorage.setItem('speedtest_lang', lang); } catch { /* ignore */ }
  applyLang();
  // === [F3: propagate lang to history panel] ===
  try { window.__historyPanel?.setLang(lang); } catch { /* defensive — panel may not be mounted */ }
  // === [F3 end] ===
});

/* ── DOM helpers ─────────────────────────────────────────────────────────── */
function setVal(id, text) { $(id).textContent = text; }
// Tracks which measurement phase the test is in. Read by the ping loop so
// each ping sample is attributed to the right direction-specific window.
let currentPhase = 'idle'; // 'idle' | 'ping' | 'download' | 'upload'

function setStage(key) {
  $('start-text').textContent = t(key);
  if      (key === 'stagePing') currentPhase = 'ping';
  else if (key === 'stageDown') currentPhase = 'download';
  else if (key === 'stageUp')   currentPhase = 'upload';
  else                          currentPhase = 'idle';
}

// === [F1: bufferbloat + slow-start state] ===
// Module-scoped state for Phase 2 measurement accuracy:
//   - idleLatencySamples: RTTs collected while currentPhase === 'ping'
//   - loadedLatencySamples: RTTs collected during 'download' / 'upload'
//   - dlThroughputSamples / ulThroughputSamples: (elapsedMs, mbps) pairs so
//     we can recompute the final number after dropping warmup samples while
//     the gauge stays live across the whole test.
// Reset at the start of every runTest() in the [F1: bufferbloat reset] block.
let idleLatencySamples   = [];
let loadedLatencySamples = [];
let dlThroughputSamples  = [];
let ulThroughputSamples  = [];

/**
 * Mean of a numeric array; 0 for empty.
 * @param {number[]} xs
 * @returns {number}
 */
function average(xs) {
  if (!xs || xs.length === 0) return 0;
  let s = 0;
  for (const x of xs) s += x;
  return s / xs.length;
}

/**
 * Map bufferbloat delta (loadedAvg - idleAvg, ms) to an A/B/C/D grade.
 * Thresholds borrowed from the DSLReports / Waveform Bufferbloat tradition:
 *   < 5 ms : A — imperceptible
 *   < 30 ms: B — acceptable, faint impact on VoIP/games
 *   < 60 ms: C — noticeable, video calls degrade
 *   ≥ 60 ms: D — severe, real-time apps unusable under load
 * @param {number} deltaMs
 * @returns {'A'|'B'|'C'|'D'}
 */
function bufferbloatGrade(deltaMs) {
  if (!Number.isFinite(deltaMs) || deltaMs < 5)  return 'A';
  if (deltaMs < 30) return 'B';
  if (deltaMs < 60) return 'C';
  return 'D';
}
// === [F1 end] ===

// Arc length of the semicircle (π × r = π × 80). Must match the SVG path.
const ARC_LENGTH = Math.PI * 80;

// Cached node refs + rAF coalesced writes: setSpeed can fire hundreds of
// times per second on fast links — batching to one DOM write per animation
// frame keeps the transition smooth and prevents the needle from "chasing"
// stale values.
const gaugeNodes = {};
const pendingSpeed = { download: null, upload: null };
let speedRafScheduled = false;

function flushSpeed() {
  speedRafScheduled = false;
  for (const prefix of ['download', 'upload']) {
    const mbps = pendingSpeed[prefix];
    if (mbps === null) continue;
    pendingSpeed[prefix] = null;

    const nodes = gaugeNodes[prefix] || (gaugeNodes[prefix] = {
      speed:  $(prefix + '-speed'),
      mb:     $(prefix + '-speed-mb'),
      fill:   $(prefix + '-fill'),
      needle: $(prefix + '-needle'),
    });

    nodes.speed.textContent = mbps.toFixed(1);
    nodes.mb.textContent    = '(' + (mbps / 8).toFixed(1) + ' MB/s)';

    const angle    = gaugeAngle(mbps);
    const offset   = ARC_LENGTH * (angle / 180);
    const rotation = 90 - angle;

    nodes.fill.style.strokeDashoffset = String(offset);
    nodes.needle.setAttribute('transform', `rotate(${rotation} 100 110)`);
  }
}

function setSpeed(prefix, mbps) {
  pendingSpeed[prefix] = mbps;
  if (speedRafScheduled) return;
  speedRafScheduled = true;
  requestAnimationFrame(flushSpeed);
}

function resetGauges() {
  ['download', 'upload'].forEach(prefix => {
    setVal(prefix + '-speed', '--');
    setVal(prefix + '-speed-mb', '');
    $(prefix + '-fill').style.strokeDashoffset = String(ARC_LENGTH);
    $(prefix + '-needle').setAttribute('transform', 'rotate(-90 100 110)');
  });
}

function resetDisplay() {
  resetGauges();
  [
    'packet-loss',
    'download-latency', 'upload-latency',
    'download-jitter',  'upload-jitter',
  ].forEach(id => setVal(id, '--'));
}

/* ── Stop mechanism ──────────────────────────────────────────────────────── */
// abortCtrl is replaced on every test run; calling .abort() cancels all
// in-flight fetch requests and signals upload loops to stop.
let abortCtrl = new AbortController();

function showStopBtn(show) {
  $('stop-btn').hidden = !show;
}

$('stop-btn').addEventListener('click', () => {
  abortCtrl.abort();
});

/* ── Latency / Jitter / Packet-loss ─────────────────────────────────────── */
// Two sample sets per ping:
//   - allSamples is a rolling window of the last PING_WINDOW pings, used
//     for the live packet-loss readout (the recent loss rate is what the
//     user is steering by during the test).
//   - dlSamples / ulSamples are *cumulative* per-phase arrays, so the
//     latency / jitter displayed at the end of the download or upload
//     phase is the mean over the whole phase rather than the last few
//     seconds (which is what a rolling window would show under load).
const PING_WINDOW   = 20;
const PING_INTERVAL = 250;

// Tracks whether we've already surfaced a network-error toast for the
// current ping loop. Pings fire every 250ms; without throttling, a
// disconnected client would spam the toast container.
let pingNetworkErrorShown = false;

async function pingOnce(signal, seq) {
  const t0 = performance.now();
  try {
    const r = await fetch('/api/ping?_=' + (Date.now() + seq), {
      cache: 'no-store',
      signal,
    });
    if (!r.ok) return { rtt: 0, ok: false };
    return { rtt: performance.now() - t0, ok: true };
  } catch (e) {
    if (e.name === 'AbortError') throw e;
    if (!pingNetworkErrorShown) {
      pingNetworkErrorShown = true;
      const info = toastForFetchError(e, 0);
      if (info) showToast(info.msg, info.level);
    }
    return { rtt: 0, ok: false };
  }
}

function renderPingStats(allSamples, dlSamples, ulSamples) {
  setVal('packet-loss', windowStats(allSamples).packetLoss.toFixed(1) + ' %');

  // Per-direction latency + jitter — averaged over every ping taken during
  // the phase (dlSamples / ulSamples are cumulative, not a rolling window),
  // so the final readout reflects the whole phase rather than the last few
  // seconds. Rendered only once each phase has samples so the previous
  // run's values aren't shown during the next phase's warmup.
  if (dlSamples.length > 0) {
    const dl = windowStats(dlSamples);
    setVal('download-latency', dl.latency.toFixed(1) + ' ms');
    const dlRtts = dlSamples.filter(s => s.ok).map(s => s.rtt);
    setVal('download-jitter',  jitterRFC3550(dlRtts).toFixed(1) + ' ms');
  }
  if (ulSamples.length > 0) {
    const ul = windowStats(ulSamples);
    setVal('upload-latency', ul.latency.toFixed(1) + ' ms');
    const ulRtts = ulSamples.filter(s => s.ok).map(s => s.rtt);
    setVal('upload-jitter',  jitterRFC3550(ulRtts).toFixed(1) + ' ms');
  }
}

// Run a background ping loop until signal is aborted. Maintains a rolling
// `allSamples` window for live packet-loss, plus *cumulative* per-direction
// arrays so end-of-phase latency / jitter average the whole phase, not just
// the trailing few seconds.
async function runPingLoop(signal) {
  let allSamples = [];
  let dlSamples  = [];
  let ulSamples  = [];
  let seq = 0;

  while (!signal.aborted) {
    try {
      const sample = await pingOnce(signal, seq++);
      allSamples = pushWindow(allSamples, sample, PING_WINDOW);
      if      (currentPhase === 'download') dlSamples = [...dlSamples, sample];
      else if (currentPhase === 'upload')   ulSamples = [...ulSamples, sample];
      // === [F1: bufferbloat sample routing] ===
      // Unlike the rolling per-direction windows above (used for live UI
      // metrics), bufferbloat needs the *full* history of idle vs loaded
      // RTTs so the end-of-test delta reflects the whole load period, not
      // just the last 20 pings. Only successful pings contribute to either
      // bucket — failed pings show up as packet loss instead.
      if (sample.ok) {
        if      (currentPhase === 'ping')                                idleLatencySamples.push(sample.rtt);
        else if (currentPhase === 'download' || currentPhase === 'upload') loadedLatencySamples.push(sample.rtt);
      }
      // === [F1 end] ===
      renderPingStats(allSamples, dlSamples, ulSamples);
    } catch (e) {
      if (e.name === 'AbortError') break;
      throw e;
    }
    if (signal.aborted) break;
    // Pace pings; abort during sleep should resolve promptly.
    await new Promise(resolve => {
      let timer;
      const onAbort = () => { clearTimeout(timer); resolve(); };
      timer = setTimeout(() => {
        signal.removeEventListener('abort', onAbort);
        resolve();
      }, PING_INTERVAL);
      signal.addEventListener('abort', onAbort, { once: true });
    });
  }
}

/* ── Download ────────────────────────────────────────────────────────────── */
// All streams share a single fixed measureStart so elapsed time is consistent
// across concurrent readers.  Warmup only applies in time mode.

const WARMUP_MS = 2000;

async function measureDownload(signal) {
  const streams    = activeCfg.streams || 1;
  const t0         = performance.now();
  // Fixed measurement start: no warmup in size mode (transfer is bounded by
  // byte count); warmup in time mode to skip TCP slow-start.
  const measureStart = activeCfg.mode === 'size' ? t0 : t0 + WARMUP_MS;
  const totalBytes   = activeCfg.downloadMB * 1024 * 1024;
  let totalReceived  = 0;

  // Client-side hard deadline for time mode. Mirrors the server's
  // SetWriteDeadline so a slow link can't keep the body open past the
  // configured duration. The 2 s grace matches the server side.
  let deadlineCtrl  = null;
  let deadlineTimer = 0;
  if (activeCfg.mode === 'time') {
    deadlineCtrl = new AbortController();
    deadlineTimer = setTimeout(
      () => deadlineCtrl.abort(),
      activeCfg.durationSecs * 1000 + 2000,
    );
    if (signal) {
      signal.addEventListener('abort', () => deadlineCtrl.abort(), { once: true });
    }
  }
  const streamSignal = deadlineCtrl ? deadlineCtrl.signal : signal;

  const streamTask = async streamIndex => {
    let url = '/api/download?_=' + Date.now() + Math.random();
    if (activeCfg.mode === 'size') {
      const bytes = Math.floor(totalBytes / streams) + (streamIndex < totalBytes % streams ? 1 : 0);
      url += '&bytes=' + bytes;
    } else {
      url += '&duration=' + activeCfg.durationSecs;
    }

    const res = await fetch(url, { cache: 'no-store', signal: streamSignal });
    if (!res.ok || !res.body) {
      const err = new Error('download failed');
      err.httpStatus = res.status;
      throw err;
    }
    const reader = res.body.getReader();

    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        const now = performance.now();
        if (now < measureStart) continue;     // warmup — discard
        totalReceived += value.byteLength;
        const elapsedMs = now - measureStart;
        const mbps      = throughputMbps(totalReceived, elapsedMs);
        setSpeed('download', mbps);
        // === [F1: slow-start trim] ===
        // Record (elapsedMs, cumulative bytes) so the final number can be
        // recomputed over only the post-warmup tail. The gauge above keeps
        // rendering every sample for liveness.
        dlThroughputSamples.push({ elapsedMs, bytes: totalReceived });
        // === [F1 end] ===
      }
    } catch (e) {
      // Deadline-triggered abort is the expected exit on slow links —
      // bytes counted so far are still a valid throughput sample. Only
      // the user-initiated abort should propagate as an error.
      if (e?.name === 'AbortError' && deadlineCtrl && !signal?.aborted) return;
      throw e;
    }
  };

  try {
    await Promise.all(Array.from({ length: streams }, (_, i) => streamTask(i)));
  } finally {
    if (deadlineTimer) clearTimeout(deadlineTimer);
  }

  // === [F1: slow-start trim — final throughput] ===
  // Recompute the reported throughput over only the post-warmup window by
  // subtracting the bytes-and-time that elapsed before the warmup boundary.
  // Falls back to the original total-bytes / total-elapsed calculation when
  // there aren't enough post-warmup samples (very short tests or slow links).
  return trimThroughput(dlThroughputSamples, totalReceived, performance.now() - measureStart);
  // === [F1 end] ===
}

// === [F1: slow-start trim helper] ===
/**
 * Compute final throughput, discarding samples taken before `srvCfg.warmupMs`
 * to remove TCP slow-start bias. If no post-warmup samples exist (test was
 * shorter than the warmup window), falls back to the unfiltered total.
 *
 * @param {{elapsedMs:number, bytes:number}[]} samples  Recorded during measurement.
 * @param {number} totalBytes  Cumulative bytes transferred at end of test.
 * @param {number} totalElapsedMs  Wall time from measureStart to test end.
 * @returns {number}  Throughput in Mbps.
 */
function trimThroughput(samples, totalBytes, totalElapsedMs) {
  const warmupMs = Number(srvCfg.warmupMs) || 0;
  if (warmupMs <= 0 || samples.length === 0) {
    return throughputMbps(totalBytes, totalElapsedMs);
  }
  // Find the first sample at or after the warmup boundary. Bytes / time
  // before that point are subtracted from the totals.
  const idx = samples.findIndex(s => s.elapsedMs >= warmupMs);
  if (idx === -1) {
    // Whole test was inside the warmup window — fall back so we never
    // report 0 just because the test was short.
    return throughputMbps(totalBytes, totalElapsedMs);
  }
  const baseline   = idx === 0 ? { elapsedMs: 0, bytes: 0 } : samples[idx - 1];
  const tailBytes  = totalBytes - baseline.bytes;
  const tailMs     = totalElapsedMs - baseline.elapsedMs;
  return throughputMbps(tailBytes, tailMs);
}
// === [F1 end] ===

/* ── Upload ──────────────────────────────────────────────────────────────── */
// Pre-generate a 1 MB random chunk reused across requests.
const CHUNK_BYTES = 1 * 1024 * 1024;
const uploadChunk = (() => {
  const buf = new Uint8Array(CHUNK_BYTES);
  const MAX_RAND = 65536;
  for (let off = 0; off < CHUNK_BYTES; off += MAX_RAND) {
    crypto.getRandomValues(buf.subarray(off, Math.min(off + MAX_RAND, CHUNK_BYTES)));
  }
  return buf;
})();

// Upload a single blob. When `deadlineMs` is finite and reached before the
// XHR completes, the request is aborted but the promise resolves cleanly —
// the bytes already sent (counted via `progress` events) stay in the
// running throughput total. This is the key fix that keeps time-mode tests
// inside the configured duration on slow uplinks: a 1 MB chunk on a 256
// Kbps link would otherwise force the loop to wait 30 s+ for a single
// blob to finish before noticing the deadline.
function postBlobUntil(blob, onProgress, signal, deadlineMs) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    let endedByDeadline = false;
    let externalAbort   = false;

    const cleanup = () => {
      clearTimeout(timer);
      if (signal) signal.removeEventListener('abort', onUserAbort);
    };

    const onUserAbort = () => { externalAbort = true; xhr.abort(); };

    xhr.upload.addEventListener('progress', onProgress);
    xhr.addEventListener('load', () => {
      cleanup();
      if (xhr.status >= 200 && xhr.status < 400) {
        // Parse the server's timing reply when available. serverElapsedMs +
        // received let the caller cross-check the client-side onProgress
        // numbers against authoritative server-observed bytes/time, side-
        // stepping any wall-clock weirdness from tab throttling.
        let server = null;
        try {
          const body = JSON.parse(xhr.responseText);
          if (body && typeof body.received === 'number' && typeof body.serverElapsedMs === 'number') {
            server = { received: body.received, serverElapsedMs: body.serverElapsedMs, truncated: !!body.truncated };
          }
        } catch { /* non-JSON or empty body — fall back to client timing */ }
        resolve({ aborted: false, server });
      } else {
        const err = new Error('upload XHR status ' + xhr.status);
        err.httpStatus = xhr.status;
        reject(err);
      }
    });
    xhr.addEventListener('abort', () => {
      cleanup();
      if (externalAbort)    reject(new DOMException('Aborted', 'AbortError'));
      else if (endedByDeadline) resolve({ aborted: true, server: null });
      else                  reject(new Error('upload XHR aborted unexpectedly'));
    });
    xhr.addEventListener('error', () => {
      cleanup();
      if (endedByDeadline) resolve({ aborted: true, server: null });
      else {
        // No usable status on a network error — fetch's TypeError analogue.
        const err = new TypeError('upload XHR error');
        reject(err);
      }
    });

    xhr.open('POST', '/api/upload?_=' + Date.now() + Math.random());

    const remaining = Number.isFinite(deadlineMs)
      ? Math.max(0, deadlineMs - performance.now())
      : Infinity;
    const timer = Number.isFinite(remaining)
      ? setTimeout(() => { endedByDeadline = true; xhr.abort(); }, remaining)
      : 0;

    if (signal) signal.addEventListener('abort', onUserAbort);

    xhr.send(blob);
  });
}

async function measureUpload(signal) {
  const streams      = activeCfg.streams || 1;
  const t0           = performance.now();
  // Same fixed-start approach as download.
  const measureStart = activeCfg.mode === 'size' ? t0 : t0 + WARMUP_MS;
  let totalSent      = 0;
  // Server-authoritative tallies (aggregated across all streams/chunks).
  // Used to override the client-timed final number when available, which
  // dodges any browser-side clock drift or tab-throttling weirdness.
  let serverReceivedTotal  = 0;
  let serverElapsedMaxMs   = 0;

  const onChunk = (delta, now) => {
    if (now < measureStart) return;
    totalSent += delta;
    const elapsedMs = now - measureStart;
    const mbps      = throughputMbps(totalSent, elapsedMs);
    setSpeed('upload', mbps);
    // === [F1: slow-start trim] ===
    // Record (elapsedMs, cumulative bytes) for the same post-warmup
    // recomputation done in measureDownload.
    ulThroughputSamples.push({ elapsedMs, bytes: totalSent });
    // === [F1 end] ===
  };

  const recordServerTiming = result => {
    if (result?.server) {
      serverReceivedTotal += result.server.received;
      // Streams run in parallel — max elapsed across streams is the true
      // wall-clock the server saw end-to-end.
      if (result.server.serverElapsedMs > serverElapsedMaxMs) {
        serverElapsedMaxMs = result.server.serverElapsedMs;
      }
    }
  };

  const workerSize = async () => {
    let remaining = Math.ceil(activeCfg.uploadMB * 1024 * 1024 / streams);
    while (remaining > 0 && !signal.aborted) {
      const slice = remaining >= CHUNK_BYTES
        ? uploadChunk
        : uploadChunk.subarray(0, remaining);
      remaining -= slice.byteLength;
      let lastLoaded = 0;
      const result = await postBlobUntil(new Blob([slice]), e => {
        if (!e.lengthComputable) return;
        onChunk(e.loaded - lastLoaded, performance.now());
        lastLoaded = e.loaded;
      }, signal, Infinity);
      recordServerTiming(result);
    }
  };

  const workerTime = async () => {
    const deadline = t0 + activeCfg.durationSecs * 1000;
    while (performance.now() < deadline && !signal.aborted) {
      let lastLoaded = 0;
      const result = await postBlobUntil(new Blob([uploadChunk]), e => {
        if (!e.lengthComputable) return;
        onChunk(e.loaded - lastLoaded, performance.now());
        lastLoaded = e.loaded;
      }, signal, deadline);
      recordServerTiming(result);
      if (result.aborted) break;     // deadline reached mid-upload
    }
  };

  const worker = activeCfg.mode === 'time' ? workerTime : workerSize;
  await Promise.all(Array.from({ length: streams }, worker));

  // === [F1: slow-start trim — final throughput] ===
  const clientMbps = trimThroughput(ulThroughputSamples, totalSent, performance.now() - measureStart);
  // Prefer the server's authoritative wall-clock when we have enough samples
  // (>= 100 ms and at least one full chunk acknowledged). For very short or
  // aborted uploads the client-side trim is more accurate because it covers
  // the in-flight bytes the server never saw acknowledged.
  if (serverReceivedTotal > 0 && serverElapsedMaxMs >= 100) {
    return throughputMbps(serverReceivedTotal, serverElapsedMaxMs);
  }
  return clientMbps;
  // === [F1 end] ===
}

/* ── [F3: collectFinalResult helper] ─────────────────────────────────────── */
// Assemble the Result JSON payload posted to /api/results when history is
// enabled. We read most metrics back from the DOM rather than threading
// them through return values — the visible numbers are what the user just
// saw, which is the contract we want to persist.
//
// F1 owns idle/loaded latency and the bufferbloat grade; these fields are
// optional in B1's schema, so missing values fall back to 0 / "".
function parseDomNumber(id) {
  const el = document.getElementById(id);
  if (!el) return 0;
  // Values render as "123.4 ms" / "950.2" / "--". A leading "--" or a
  // non-numeric prefix yields 0 from parseFloat, which is exactly what we
  // want to send as the "not measured" sentinel.
  const n = parseFloat(el.textContent);
  return Number.isFinite(n) ? n : 0;
}

function collectFinalResult() {
  // Packet loss is rendered as "x.y %"; parseFloat strips the unit.
  const packetLoss     = parseDomNumber('packet-loss');
  const downloadMbps   = parseDomNumber('download-speed');
  const uploadMbps     = parseDomNumber('upload-speed');
  const dlLatency      = parseDomNumber('download-latency');
  const ulLatency      = parseDomNumber('upload-latency');
  const dlJitter       = parseDomNumber('download-jitter');
  const ulJitter       = parseDomNumber('upload-jitter');
  // F1 may not have shipped yet — fall back to "" so the schema stays valid.
  // F1 renders the grade as "A (+1 ms)" / "B (+27 ms)" / etc, so we extract
  // the leading letter rather than requiring an exact one-char match.
  const gradeEl        = document.getElementById('bufferbloat-grade');
  const gradeMatch     = gradeEl?.textContent?.trim().match(/^([A-D])\b/);
  const grade          = gradeMatch ? gradeMatch[1] : '';
  // "Idle" baseline is the minimum of the two per-direction averages —
  // when F1 isn't shipped we just use the smaller of dl/ul latency.
  const idleLatency    = Math.min(
    dlLatency > 0 ? dlLatency : Infinity,
    ulLatency > 0 ? ulLatency : Infinity,
  );
  const loadedLatency  = Math.max(dlLatency, ulLatency);

  return {
    download_mbps:      downloadMbps,
    upload_mbps:        uploadMbps,
    latency_idle_ms:    Number.isFinite(idleLatency) ? idleLatency : 0,
    latency_loaded_ms:  loadedLatency,
    download_jitter_ms: dlJitter,
    upload_jitter_ms:   ulJitter,
    packet_loss:        packetLoss,
    bufferbloat_grade:  grade,
    settings_json:      JSON.stringify({
      mode:     activeCfg.mode,
      duration: activeCfg.durationSecs,
      streams:  activeCfg.streams,
    }),
  };
}
/* ── [F3 end] ─────────────────────────────────────────────────────────── */

/* ── Orchestrate full test ───────────────────────────────────────────────── */
async function runTest() {
  if (testing) return;
  testing = true;

  // Fresh abort controller for this run.
  abortCtrl = new AbortController();
  const { signal } = abortCtrl;

  // Separate controller for the background ping loop so we can stop it
  // independently from the user's "Stop" button (which aborts everything).
  const pingCtrl = new AbortController();
  const stopPing = () => pingCtrl.abort();
  signal.addEventListener('abort', stopPing);

  const btn = $('start-btn');
  btn.disabled = true;
  showStopBtn(true);
  resetDisplay();
  // Allow the ping loop to surface one fresh network-error toast per run.
  pingNetworkErrorShown = false;

  // === [F1: bufferbloat reset] ===
  // Wipe Phase 2 state from the previous run so the new test starts clean.
  // Note: empty-array assignments here are intentional — these arrays are
  // module-scoped and shared with runPingLoop / measureDownload /
  // measureUpload, so we replace the reference rather than mutating the
  // old one (other closures may still hold the previous array briefly).
  idleLatencySamples   = [];
  loadedLatencySamples = [];
  dlThroughputSamples  = [];
  ulThroughputSamples  = [];
  const bbCell  = $('bufferbloat-grade-cell');
  const bbGrade = $('bufferbloat-grade');
  if (bbCell)  bbCell.hidden = true;
  if (bbGrade) bbGrade.textContent = '--';
  // === [F1 end] ===

  // Background ping loop — runs for the entire test so latency/jitter/loss
  // update continuously (and capture latency-under-load when download or
  // upload phases saturate the link).
  const pingPromise = runPingLoop(pingCtrl.signal).catch(err => {
    if (err.name !== 'AbortError') console.error('Ping loop error:', err);
  });

  try {
    setStage('stagePing');
    // === [F1: idle-latency baseline] ===
    // setStage('stagePing') above set currentPhase = 'ping', so the ping
    // loop will route the next ~1.5s of samples into idleLatencySamples
    // (see bufferbloat sample routing in runPingLoop).
    // === [F1 end] ===
    // Warm up the ping window before kicking off throughput tests so the
    // first metrics shown are based on real samples, not zeros.
    await new Promise(resolve => setTimeout(resolve, 1500));

    if (!signal.aborted) {
      setStage('stageDown');
      await measureDownload(signal);
    }

    if (!signal.aborted) {
      setStage('stageUp');
      await measureUpload(signal);
    }

    // === [F1: bufferbloat grade compute] ===
    // Only compute when both buckets actually have samples — otherwise the
    // grade is meaningless and showing "A" by default would be misleading.
    if (idleLatencySamples.length > 0 && loadedLatencySamples.length > 0) {
      const idleAvg   = average(idleLatencySamples);
      const loadedAvg = average(loadedLatencySamples);
      const bbDelta   = Math.max(0, loadedAvg - idleAvg);
      const grade     = bufferbloatGrade(bbDelta);
      const bbGradeEl = $('bufferbloat-grade');
      const bbCellEl  = $('bufferbloat-grade-cell');
      if (bbGradeEl) bbGradeEl.textContent = `${grade} (+${bbDelta.toFixed(0)} ms)`;
      if (bbCellEl)  bbCellEl.hidden = false;
    }
    // === [F1 end] ===

    // === [F3: persist result] ===
    // POST the final result to B1's /api/results so it shows up in the
    // history drawer + trends panel. Gated on the server feature flag.
    // Persistence failures are logged and otherwise ignored — the test
    // itself succeeded, so we must not show the user a destructive error.
    if (srvCfg?.historyEnabled && !signal.aborted) {
      try {
        const result = collectFinalResult();
        const r = await fetch('/api/results', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(result),
          signal,
        });
        if (!r.ok) {
          console.warn('persist result: server returned', r.status);
        }
        window.__historyPanel?.refresh().catch(() => {});
      } catch (err) {
        if (err && err.name !== 'AbortError') {
          console.error('persist result failed', err);
        }
      }
    }
    // === [F3 end] ===

  } catch (err) {
    if (err.name !== 'AbortError') {
      console.error('Speedtest error:', err);
      const info = toastForFetchError(err, err && err.httpStatus);
      if (info) {
        // 503 → offer a Retry action so the user can immediately try again.
        const opts = info.level === 'warn' && err && err.httpStatus === 503
          ? { actionLabel: t('retry'), onAction: () => runTest() }
          : {};
        showToast(info.msg, info.level, opts);
      }
    }
  } finally {
    stopPing();
    await pingPromise;
    signal.removeEventListener('abort', stopPing);

    testing = false;
    btn.disabled = false;
    showStopBtn(false);
    setStage('done');
  }
}

$('start-btn').addEventListener('click', runTest);

/* ── Init ────────────────────────────────────────────────────────────────── */
document.addEventListener('DOMContentLoaded', async () => {
  // Tooltip popover is mounted first so that the language pass below
  // (which calls refreshTooltips) finds the overlay already wired up.
  mountTooltips();
  applyLang();

  // Mount the toast container as early as possible so subsequent bootstrap
  // failures (config, IP) can surface to the user.
  const toastContainer = $('toast-container');
  if (toastContainer) showToast = mountToast(toastContainer);

  // Load server config
  try {
    const r = await fetch('/api/config');
    if (r.ok) srvCfg = await r.json();
    else {
      const info = toastForFetchError(null, r.status);
      if (info) showToast(info.msg, info.level);
    }
  } catch (err) {
    const info = toastForFetchError(err, 0);
    if (info) showToast(info.msg, info.level);
  }

  // Merge: localStorage > server > defaults
  const localCfg = loadLocalConfig();
  activeCfg = mergeConfig(srvCfg, localCfg);

  // Init UI and apply merged config
  initConfigUI();
  applyConfigToUI(activeCfg);

  // Load client IP
  try {
    const r = await fetch('/api/ip');
    if (r.ok) {
      const d = await r.json();
      const ip = d.ip ?? '--';
      setVal('ip-address', ip);
      setVal('connection-type', ip.includes(':') ? 'IPv6' : 'IPv4');
    }
  } catch { /* IP is a nice-to-have; don't toast on its failure */ }


  // === [F3: mount history drawer] ===
  // Gated on the server-side feature flag (`historyEnabled` from /api/config).
  // When disabled the panel stays hidden — preserves the zero-config /
  // no-DB single-binary deployment story.
  if (srvCfg?.historyEnabled) {
    const historyEl = $('history-drawer');
    if (historyEl) {
      historyEl.hidden = false;
      // Stash on window so runTest's [F3: persist result] block can call
      // refresh() without re-importing or threading through closures.
      window.__historyPanel = mountHistory(historyEl, { apiBase: '/api/results', pageSize: 20, lang });
    }
  }
  // === [F3 end] ===
});
