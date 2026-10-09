/* Static reader only. Never cache API responses, the signed-in app or notes. */
const READER_CACHE = 'gnotes-offline-reader-v1';
const FILES = ['/offline.html', '/offline.css', '/offline-store.js', '/offline-reader.js'];
self.addEventListener('install', event => {
  event.waitUntil((async () => {
    const cache = await caches.open(READER_CACHE);
    await cache.addAll(FILES.map(path => new Request(path, { cache: 'reload', credentials: 'omit' })));
    await self.skipWaiting();
  })());
});
self.addEventListener('activate', event => {
  event.waitUntil((async () => {
    for (const name of await caches.keys()) {
      if (name.startsWith('gnotes-offline-reader-') && name !== READER_CACHE) await caches.delete(name);
    }
    await self.clients.claim();
  })());
});
self.addEventListener('fetch', event => {
  const { request } = event;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin || request.method !== 'GET') return;
  if (request.mode === 'navigate' && ['/', '/index.html', '/offline.html'].includes(url.pathname) && !url.search) {
    event.respondWith((async () => {
      try {
        const response = await fetch(request, { cache: 'no-store' });
        if (response.status < 500) return response;
      } catch { /* No network: open the locked reader. */ }
      return (await caches.open(READER_CACHE)).match('/offline.html');
    })());
  } else if (FILES.includes(url.pathname) && !url.search) {
    event.respondWith((async () => (await (await caches.open(READER_CACHE)).match(url.pathname)) || fetch(request))());
  }
});
