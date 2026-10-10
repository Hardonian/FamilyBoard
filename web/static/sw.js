// FamilyBoard Service Worker - Appliance & Mobile Offline Resilience
const CACHE_NAME = 'familyboard-shell-v1.2.0';
const STATIC_ASSETS = [
  '/',
  '/static/css/app.css',
  '/static/js/kiosk.js',
  '/static/icons/icon.svg',
  '/static/icons/icon-192.png',
  '/static/icons/icon-512.png',
  '/static/icons/apple-touch-icon.png',
  '/manifest.webmanifest'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(STATIC_ASSETS).catch((err) => {
        console.warn('[SW] Cache addAll partial failure:', err);
      });
    }).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.map((key) => {
          if (key !== CACHE_NAME) {
            return caches.delete(key);
          }
        })
      );
    }).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Never intercept or cache SSE streams or write mutations
  if (url.pathname.includes('/api/events/stream') || event.request.method !== 'GET') {
    return;
  }

  // Navigation requests (HTML document): Network-first with cache fallback
  if (event.request.mode === 'navigate') {
    event.respondWith(
      fetch(event.request)
        .then((networkResp) => {
          if (networkResp.ok) {
            const copy = networkResp.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
          }
          return networkResp;
        })
        .catch(async () => {
          const cached = await caches.match(event.request);
          if (cached) return cached;
          const rootCached = await caches.match('/');
          if (rootCached) return rootCached;
          return new Response('Offline - please reconnect to home Wi-Fi', {
            status: 503,
            headers: { 'Content-Type': 'text/plain' }
          });
        })
    );
    return;
  }

  // API calls: Network-first, fallback to cache for offline tolerance
  if (url.pathname.startsWith('/api/')) {
    event.respondWith(
      fetch(event.request)
        .then((resp) => {
          if (resp.ok) {
            const copy = resp.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, copy));
          }
          return resp;
        })
        .catch(async () => {
          const cached = await caches.match(event.request);
          if (cached) return cached;
          return new Response(JSON.stringify([]), {
            status: 200,
            headers: { 'Content-Type': 'application/json' }
          });
        })
    );
    return;
  }

  // Static assets: Cache-first, fallback to network
  event.respondWith(
    caches.match(event.request).then((cached) => {
      if (cached) {
        // Fetch in background to update cache if fresh
        fetch(event.request).then((netResp) => {
          if (netResp.ok) {
            caches.open(CACHE_NAME).then((c) => c.put(event.request, netResp));
          }
        }).catch(() => {});
        return cached;
      }
      return fetch(event.request).then((netResp) => {
        if (netResp.ok && event.request.method === 'GET') {
          const copy = netResp.clone();
          caches.open(CACHE_NAME).then((c) => c.put(event.request, copy));
        }
        return netResp;
      });
    })
  );
});
