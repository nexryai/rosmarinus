/// <reference types="node" />

import { describe, expect, it, vi } from "vitest";

import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

type WorkerEvent = { request: { method: string; mode: string; url: string }; respondWith: ReturnType<typeof vi.fn> };

const loadFetchHandler = () => {
    const listeners = new Map<string, (event: WorkerEvent) => void>();
    const worker = {
        addEventListener: (type: string, listener: (event: WorkerEvent) => void) => listeners.set(type, listener),
        clients: { claim: vi.fn() },
        location: { origin: "https://rosemary.example" },
        skipWaiting: vi.fn(),
    };
    runInNewContext(readFileSync("public/sw.js", "utf8"), {
        URL,
        caches: { delete: vi.fn(), keys: vi.fn(), match: vi.fn(), open: vi.fn() },
        fetch: vi.fn(() => new Promise(() => undefined)),
        self: worker,
    });
    return listeners.get("fetch") as (event: WorkerEvent) => void;
};

describe("Rosemary service worker", () => {
    it("intercepts app-shell routes but leaves backend routes untouched", () => {
        const handleFetch = loadFetchHandler();
        const appRespondWith = vi.fn();
        handleFetch({ request: { method: "GET", mode: "navigate", url: "https://rosemary.example/settings" }, respondWith: appRespondWith });
        expect(appRespondWith).toHaveBeenCalledOnce();

        for (const path of ["/api/v1/session", "/users/alice", "/notes/note-id", "/media/file-id", "/.well-known/webfinger"]) {
            const respondWith = vi.fn();
            handleFetch({ request: { method: "GET", mode: "navigate", url: `https://rosemary.example${path}` }, respondWith });
            expect(respondWith).not.toHaveBeenCalled();
        }
    });
});
