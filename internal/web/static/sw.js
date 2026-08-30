// Service Worker for Прозоро PWA
//
// Правила кэширования (исправлено 28.08.2026):
//   • /api/* — ВСЕГДА сеть, без кэширования (кэшировать API нельзя:
//     устаревшие/ошибочные ответы застревали в кэше и ломали приложение);
//   • навигация (/) — network-first с фолбэком на кэш (офлайн);
//   • статика — cache-first.
// Версия кэша повышена — старые записи будут удалены при активации.

const CACHE_NAME = "prozoro-v6";
const STATIC_ASSETS = ["/manifest.json", "/icons/icon.svg"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(STATIC_ASSETS))
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => k !== CACHE_NAME)
          .map((k) => caches.delete(k))
      )
    )
  );
  self.clients.claim();
});

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);

  // API — только сеть: никаких кэшей и никаких фолбэков на кэш
  if (url.pathname.startsWith("/api/")) {
    event.respondWith(fetch(event.request));
    return;
  }

  // Навигация (HTML) — network-first, офлайн-фолбэк на кэш
  if (event.request.mode === "navigate" || event.request.destination === "document") {
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          const clone = response.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
          return response;
        })
        .catch(() => caches.match(event.request))
    );
    return;
  }

  // Статика — cache-first
  event.respondWith(
    caches.match(event.request).then((cached) => cached || fetch(event.request))
  );
});
