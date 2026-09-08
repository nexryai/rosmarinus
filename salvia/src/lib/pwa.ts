export function registerServiceWorker() {
    if (!("serviceWorker" in navigator)) return;

    window.addEventListener(
        "load",
        () => {
            void navigator.serviceWorker.register("/sw.js", { scope: "/", updateViaCache: "none" }).catch((error: unknown) => {
                console.warn("Rosemary service worker registration failed", error);
            });
        },
        { once: true },
    );
}
