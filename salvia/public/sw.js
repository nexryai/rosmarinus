const cacheName = "rosemary-shell-v2";
const staticPaths = ["/", "/manifest.webmanifest", "/favicon.svg", "/icons/rosemary-180.png", "/icons/rosemary-192.png", "/icons/rosemary-512.png", "/icons/rosemary-maskable-512.png"];

self.addEventListener("install", (event) => {
    event.waitUntil(
        caches.open(cacheName).then(async (cache) => {
            const indexResponse = await fetch("/", { cache: "reload" });
            const indexText = await indexResponse.clone().text();
            const assetPaths = [...indexText.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map((match) => match[1]);
            await cache.put("/", indexResponse);
            await cache.addAll([...staticPaths.slice(1), ...assetPaths]);
        }),
    );
    self.skipWaiting();
});

self.addEventListener("activate", (event) => {
    event.waitUntil(
        caches
            .keys()
            .then((keys) => Promise.all(keys.filter((key) => key.startsWith("rosemary-shell-") && key !== cacheName).map((key) => caches.delete(key))))
            .then(() => self.clients.claim()),
    );
});

self.addEventListener("fetch", (event) => {
    const request = event.request;
    const url = new URL(request.url);
    if (request.method !== "GET" || url.origin !== self.location.origin || url.pathname.startsWith("/api/")) return;

    if (request.mode === "navigate") {
        const appRoute = ["/", "/public", "/users", "/notifications", "/follow-requests", "/settings"].includes(url.pathname) || url.pathname.startsWith("/profiles/");
        if (!appRoute) return;
        event.respondWith(
            fetch(request)
                .then(async (response) => {
                    if (response.ok) await (await caches.open(cacheName)).put(request, response.clone());
                    return response;
                })
                .catch(async () => (await caches.match(request)) || (await caches.match("/"))),
        );
        return;
    }

    if (url.pathname.startsWith("/assets/") || url.pathname.startsWith("/icons/") || url.pathname === "/favicon.svg" || url.pathname === "/manifest.webmanifest") {
        event.respondWith(
            caches.match(request).then(
                (cached) =>
                    cached ||
                    fetch(request).then(async (response) => {
                        if (response.ok) await (await caches.open(cacheName)).put(request, response.clone());
                        return response;
                    }),
            ),
        );
    }
});
