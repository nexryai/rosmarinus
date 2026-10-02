import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

import { fileURLToPath, URL } from "node:url";

// The salvia-develop command injects this so its dummy API can use any port.
// Production builds never use the dev server proxy.
const apiProxyTarget = process.env.VITE_API_PROXY_TARGET || "http://127.0.0.1:3000";

export default defineConfig({
    plugins: [react()],
    resolve: {
        alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
    },
    build: {
        emptyOutDir: true,
        outDir: "../internal/salvia/dist",
    },
    server: {
        proxy: {
            "/api": apiProxyTarget,
        },
    },
    test: {
        environment: "jsdom",
        setupFiles: "./src/test/setup.ts",
    },
});
