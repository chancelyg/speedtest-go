/* Service worker for Speedtest PWA.
 *
 * Strategy: cache-first for the static app shell so the site loads instantly
 * and works offline (without a recorded history, but the UI shell is fine).
 *
 * Why cache-first and not stale-while-revalidate (SWR):
 *   - The app shell is shipped together with the Go binary via //go:embed.
 *     A new shell can only appear when the operator rebuilds and restarts
 *     the server, which is precisely when we bump CACHE_NAME below.
 *     Between releases the assets are immutable, so revalidating on every
 *     navigation just costs a network round-trip for no gain.
 *   - SWR would also keep serving stale JS/CSS for one full reload after a
 *     server upgrade, which is exactly the footgun we want to avoid: a
 *     version-skewed shell talking to a newer /api surface produces
 *     confusing errors. Cache-first + manual CACHE_NAME bump makes the
 *     refresh boundary explicit.
 *   - /api/* is never cached (see below), so live speed-test data and the
 *     history list are always fresh regardless of the shell cache.
 */

const CACHE_NAME = 'speedtest-v1';

const STATIC_ASSETS = [
  '/',
  '/index.html',
  '/app.js',
  '/style.css',
  '/manifest.json',
  '/history.mjs',
  '/jitter.mjs',
  '/metrics.mjs',
  '/toast.mjs',
  '/icons/favicon-192.svg',
  '/icons/favicon-512.svg',
  '/favicon.ico',
];

self.addEventListener('install', (event) => {
  // Pre-cache the entire app shell so the first offline reload after install
  // already has everything it needs. addAll is atomic: if any asset 404s the
  // install fails and the SW is not promoted, which is the correct behaviour
  // (a half-installed cache would just hide bugs).
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS))
  );
  // Skip the default "waiting" phase so the new SW activates immediately on
  // first install. Subsequent upgrades still go through the normal
  // install → wait-for-old-clients → activate cycle.
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  // Sweep any caches whose name doesn't match the current version. This is
  // the entire upgrade story: bump CACHE_NAME in the source, rebuild the
  // binary, and the next activation deletes stale shells.
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((key) => key !== CACHE_NAME)
          .map((key) => caches.delete(key))
      )
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;

  // 1. Never intercept non-GET. POST /api/results, future PUT/DELETE, etc.
  //    must hit the network unconditionally.
  if (req.method !== 'GET') return;

  let url;
  try {
    url = new URL(req.url);
  } catch (_) {
    return;
  }

  // 2. Only handle http(s). chrome-extension://, data:, blob:, file:// and
  //    similar schemes can show up when the user has devtools or extensions
  //    open; caches.put() rejects opaque/unsupported schemes with a
  //    TypeError that surfaces as an "Uncaught (in promise)" warning in the
  //    console. Guarding here keeps the SW console clean and avoids
  //    accidentally caching extension-injected resources that have nothing
  //    to do with our app.
  if (url.protocol !== 'http:' && url.protocol !== 'https:') return;

  // 3. Speed-test endpoints MUST hit the real network. Anything under /api/
  //    is either a measurement (download/upload/ping → must observe real
  //    network conditions) or live state (history list, config) we never
  //    want to read from a stale cache.
  if (url.pathname.startsWith('/api/')) return;

  // 4. /healthz is a liveness probe — never cache.
  if (url.pathname === '/healthz') return;

  // Cache-first for everything else.
  event.respondWith(
    caches.match(req).then((cached) => {
      if (cached) return cached;
      return fetch(req).then((resp) => {
        // Only cache successful, basic (same-origin) responses. Opaque
        // cross-origin responses can poison the cache because their bodies
        // are unreadable and their statuses are always 0.
        if (
          resp &&
          resp.status === 200 &&
          resp.type === 'basic'
        ) {
          const copy = resp.clone();
          caches.open(CACHE_NAME).then((cache) => {
            cache.put(req, copy).catch(() => {
              // Swallow: a put() failure (quota exceeded, unsupported
              // scheme that slipped past the guard above) must never
              // break the user-visible response.
            });
          });
        }
        return resp;
      }).catch(() => {
        // Network failed and we have no cached copy. Returning undefined
        // lets the browser surface its native offline error rather than us
        // fabricating a misleading response.
        return cached;
      });
    })
  );
});
