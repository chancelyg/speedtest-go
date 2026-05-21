// Phase 4 Track D — service worker.
//
// Contract for agent D:
//
// Cache strategy:
//   - cache-first for STATIC assets: '/', '/index.html', '/app.js',
//     '/style.css', '/manifest.json', '/icons/*', '/chart.mjs',
//     '/history.mjs', '/metrics.mjs', '/toast.mjs'
//   - NEVER cache anything under '/api/'. Speed tests must always hit the
//     network — a cached /api/download or /api/ping would invalidate
//     every measurement.
//   - Bump CACHE_NAME on each shipped release to force eviction.
//
// Lifecycle:
//   - install: cache the static asset list above
//   - activate: delete any caches whose name != CACHE_NAME
//   - fetch: bypass for /api/*, cache-first for the static set
//
// Skeleton placeholder — the real implementation lands via agent D.

const CACHE_NAME = 'speedtest-v0';

self.addEventListener('install', (event) => {
  // agent D fills cache prewarm here
  void event;
});

self.addEventListener('activate', (event) => {
  // agent D fills old-cache cleanup here
  void event;
});

self.addEventListener('fetch', (event) => {
  // Default: passthrough. Agent D implements cache-first for static + skip
  // for /api/*.
  void event;
});
