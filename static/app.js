'use strict';

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
function setBar(id, pct)  { $(id).style.width = Math.min(pct, 100) + '%'; }
function setStage(key)    { $('start-text').textContent = t(key); }

function setSpeed(prefix, mbps) {
  setVal(prefix + '-speed', mbps.toFixed(1));
  setVal(prefix + '-speed-mb', '(' + (mbps / 8).toFixed(1) + ' MB/s)');
  setBar(prefix + '-bar', (mbps / MAX_MBPS) * 100);
}

const MAX_MBPS = 1000;

function resetDisplay() {
  ['download-speed', 'upload-speed'].forEach(id => setVal(id, '--'));
  ['download-speed-mb', 'upload-speed-mb'].forEach(id => setVal(id, ''));
  ['latency', 'jitter', 'packet-loss'].forEach(id => setVal(id, '--'));
  setBar('download-bar', 0);
  setBar('upload-bar', 0);
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
const PING_COUNT = 20;

async function measurePing(signal) {
  const rtts = [];
  let failed = 0;

  for (let i = 0; i < PING_COUNT; i++) {
    if (signal.aborted) break;
    const t0 = performance.now();
    try {
      const r = await fetch('/api/ping?_=' + (Date.now() + i), { cache: 'no-store', signal });
      if (!r.ok) throw new Error('ping failed');
      rtts.push(performance.now() - t0);
    } catch {
      failed++;
    }
  }

  if (rtts.length === 0) return { latency: 0, jitter: 0, packetLoss: 100 };

  const avg      = rtts.reduce((a, b) => a + b, 0) / rtts.length;
  const variance = rtts.reduce((a, b) => a + (b - avg) ** 2, 0) / rtts.length;

  return {
    latency:    avg,
    jitter:     Math.sqrt(variance),
    packetLoss: (failed / PING_COUNT) * 100,
  };
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
      const elapsed = (now - measureStart) / 1000;
      if (elapsed > 0) {
        setSpeed('download', (totalReceived * 8) / (elapsed * 1e6));
      }
    }
  };

  await Promise.all(Array.from({ length: streams }, (_, i) => streamTask(i)));

  const elapsed = (performance.now() - measureStart) / 1000;
  return elapsed > 0 ? (totalReceived * 8) / (elapsed * 1e6) : 0;
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

function postBlob(blob, onProgress, signal) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.upload.addEventListener('progress', onProgress);
    xhr.addEventListener('load', () => {
      try { resolve(JSON.parse(xhr.responseText)); }
      catch { resolve({}); }
    });
    xhr.addEventListener('error', () => reject(new Error('upload XHR error')));
    xhr.open('POST', '/api/upload?_=' + Date.now() + Math.random());

    // Honour AbortSignal for XHR (XHR has no native signal support; we poll).
    if (signal) {
      signal.addEventListener('abort', () => { xhr.abort(); reject(new DOMException('Aborted', 'AbortError')); });
    }

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
    const elapsed = (now - measureStart) / 1000;
    if (elapsed > 0) {
      setSpeed('upload', (totalSent * 8) / (elapsed * 1e6));
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
      await postBlob(new Blob([slice]), e => {
        if (!e.lengthComputable) return;
        onChunk(e.loaded - lastLoaded, performance.now());
        lastLoaded = e.loaded;
      }, signal);
    }
  };

  const workerTime = async () => {
    const deadline = t0 + activeCfg.durationSecs * 1000;
    while (performance.now() < deadline && !signal.aborted) {
      let lastLoaded = 0;
      await postBlob(new Blob([uploadChunk]), e => {
        if (!e.lengthComputable) return;
        onChunk(e.loaded - lastLoaded, performance.now());
        lastLoaded = e.loaded;
      }, signal);
    }
  };

  const worker = activeCfg.mode === 'time' ? workerTime : workerSize;
  await Promise.all(Array.from({ length: streams }, worker));

  const elapsed = (performance.now() - measureStart) / 1000;
  return elapsed > 0 ? (totalSent * 8) / (elapsed * 1e6) : 0;
}

/* ── Orchestrate full test ───────────────────────────────────────────────── */
async function runTest() {
  if (testing) return;
  testing = true;

  // Fresh abort controller for this run.
  abortCtrl = new AbortController();
  const { signal } = abortCtrl;

  const btn = $('start-btn');
  btn.disabled = true;
  showStopBtn(true);
  resetDisplay();

  try {
    // 1. Ping
    setStage('stagePing');
    const { latency, jitter, packetLoss } = await measurePing(signal);
    if (!signal.aborted) {
      setVal('latency',     latency.toFixed(1)    + ' ms');
      setVal('jitter',      jitter.toFixed(1)     + ' ms');
      setVal('packet-loss', packetLoss.toFixed(1) + ' %');
    }

    // 2. Download
    if (!signal.aborted) {
      setStage('stageDown');
      await measureDownload(signal);
    }

    // 3. Upload
    if (!signal.aborted) {
      setStage('stageUp');
      await measureUpload(signal);
    }

  } catch (err) {
    if (err.name !== 'AbortError') console.error('Speedtest error:', err);
  } finally {
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
