'use strict';

import { gaugeAngle, windowStats, pushWindow, throughputMbps } from './metrics.mjs';

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

function mergeConfig(serverCfg, localCfg) {
  const defaults = { mode: 'time', durationSecs: 15, downloadMB: 25, uploadMB: 10, streams: 4 };
  return {
    mode:         localCfg?.mode         || serverCfg.mode         || defaults.mode,
    durationSecs: Number(localCfg?.durationSecs || serverCfg.durationSecs || defaults.durationSecs),
    downloadMB:   Number(localCfg?.downloadMB   || serverCfg.downloadMB   || defaults.downloadMB),
    uploadMB:     Number(localCfg?.uploadMB     || serverCfg.uploadMB     || defaults.uploadMB),
    streams:      Number(localCfg?.streams      || serverCfg.streams      || defaults.streams),
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
  $('latency-label').textContent     = t('latency');
  $('jitter-label').textContent      = t('jitter');
  $('packet-loss-label').textContent = t('packetLoss');
  $('ip-label').textContent          = t('ip');
  $('connection-label').textContent  = t('connection');
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
});

/* ── DOM helpers ─────────────────────────────────────────────────────────── */
function setVal(id, text) { $(id).textContent = text; }
function setStage(key)    { $('start-text').textContent = t(key); }

// Arc length of the semicircle (π × r = π × 80). Must match the SVG path.
const ARC_LENGTH = Math.PI * 80;

function setSpeed(prefix, mbps) {
  setVal(prefix + '-speed', mbps.toFixed(1));
  setVal(prefix + '-speed-mb', '(' + (mbps / 8).toFixed(1) + ' MB/s)');

  const angle    = gaugeAngle(mbps);
  // arc fill: dashoffset goes from full (empty) to 0 (full)
  const offset   = ARC_LENGTH * (angle / 180);
  // needle rotation: gaugeAngle=180 → −90° (left); 90° → 0 (up); 0° → +90° (right)
  const rotation = 90 - angle;

  $(prefix + '-fill').style.strokeDashoffset = String(offset);
  $(prefix + '-needle').setAttribute(
    'transform', `rotate(${rotation} 100 110)`
  );
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
  ['latency', 'jitter', 'packet-loss'].forEach(id => setVal(id, '--'));
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
// Rolling window of the last PING_WINDOW samples. Each ping appends a
// { rtt, ok } record; UI metrics are recomputed from the window so that
// latency/jitter/loss update continuously through the test — including
// while download and upload phases are competing for bandwidth (which is
// exactly when latency-under-load matters).
const PING_WINDOW   = 20;
const PING_INTERVAL = 250;

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
    return { rtt: 0, ok: false };
  }
}

function renderPingStats(samples) {
  const { latency, jitter, packetLoss } = windowStats(samples);
  setVal('latency',     latency.toFixed(1)    + ' ms');
  setVal('jitter',      jitter.toFixed(1)     + ' ms');
  setVal('packet-loss', packetLoss.toFixed(1) + ' %');
}

// Run a background ping loop until signal is aborted. Returns the final
// window so callers can compute summary statistics.
async function runPingLoop(signal) {
  let samples = [];
  let seq = 0;

  while (!signal.aborted) {
    try {
      const sample = await pingOnce(signal, seq++);
      samples = pushWindow(samples, sample, PING_WINDOW);
      renderPingStats(samples);
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
  return samples;
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

  const streamTask = async streamIndex => {
    let url = '/api/download?_=' + Date.now() + Math.random();
    if (activeCfg.mode === 'size') {
      const bytes = Math.floor(totalBytes / streams) + (streamIndex < totalBytes % streams ? 1 : 0);
      url += '&bytes=' + bytes;
    } else {
      url += '&duration=' + activeCfg.durationSecs;
    }

    const res = await fetch(url, { cache: 'no-store', signal });
    if (!res.ok || !res.body) throw new Error('download failed');
    const reader = res.body.getReader();

    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const now = performance.now();
      if (now < measureStart) continue;     // warmup — discard
      totalReceived += value.byteLength;
      setSpeed('download', throughputMbps(totalReceived, now - measureStart));
    }
  };

  await Promise.all(Array.from({ length: streams }, (_, i) => streamTask(i)));

  return throughputMbps(totalReceived, performance.now() - measureStart);
}

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
    xhr.addEventListener('load',  () => { cleanup(); resolve({ aborted: false }); });
    xhr.addEventListener('abort', () => {
      cleanup();
      if (externalAbort)    reject(new DOMException('Aborted', 'AbortError'));
      else if (endedByDeadline) resolve({ aborted: true });
      else                  reject(new Error('upload XHR aborted unexpectedly'));
    });
    xhr.addEventListener('error', () => {
      cleanup();
      if (endedByDeadline) resolve({ aborted: true });
      else                 reject(new Error('upload XHR error'));
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

  const onChunk = (delta, now) => {
    if (now < measureStart) return;
    totalSent += delta;
    setSpeed('upload', throughputMbps(totalSent, now - measureStart));
  };

  const workerSize = async () => {
    let remaining = Math.ceil(activeCfg.uploadMB * 1024 * 1024 / streams);
    while (remaining > 0 && !signal.aborted) {
      const slice = remaining >= CHUNK_BYTES
        ? uploadChunk
        : uploadChunk.subarray(0, remaining);
      remaining -= slice.byteLength;
      let lastLoaded = 0;
      await postBlobUntil(new Blob([slice]), e => {
        if (!e.lengthComputable) return;
        onChunk(e.loaded - lastLoaded, performance.now());
        lastLoaded = e.loaded;
      }, signal, Infinity);
    }
  };

  const workerTime = async () => {
    const deadline = t0 + activeCfg.durationSecs * 1000;
    while (performance.now() < deadline && !signal.aborted) {
      let lastLoaded = 0;
      const { aborted } = await postBlobUntil(new Blob([uploadChunk]), e => {
        if (!e.lengthComputable) return;
        onChunk(e.loaded - lastLoaded, performance.now());
        lastLoaded = e.loaded;
      }, signal, deadline);
      if (aborted) break;     // deadline reached mid-upload
    }
  };

  const worker = activeCfg.mode === 'time' ? workerTime : workerSize;
  await Promise.all(Array.from({ length: streams }, worker));

  return throughputMbps(totalSent, performance.now() - measureStart);
}

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

  // Background ping loop — runs for the entire test so latency/jitter/loss
  // update continuously (and capture latency-under-load when download or
  // upload phases saturate the link).
  const pingPromise = runPingLoop(pingCtrl.signal).catch(err => {
    if (err.name !== 'AbortError') console.error('Ping loop error:', err);
  });

  try {
    setStage('stagePing');
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

  } catch (err) {
    if (err.name !== 'AbortError') console.error('Speedtest error:', err);
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
  applyLang();

  // Load server config
  try {
    const r = await fetch('/api/config');
    if (r.ok) srvCfg = await r.json();
  } catch { /* use defaults */ }

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
  } catch { /* ignore */ }
});
