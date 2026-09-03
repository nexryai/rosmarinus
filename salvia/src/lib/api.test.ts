import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, api } from "./api";

const jsonResponse = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), {
        headers: { "Content-Type": "application/json" },
        status,
    });

describe("Rosmarinus API client", () => {
    afterEach(() => vi.restoreAllMocks());

    it("validates the versioned response envelope", async () => {
        vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ version: 2, data: { setup_required: true } })));

        await expect(api.setupStatus()).rejects.toThrow();
    });

    it("adds CSRF and idempotency headers to Actor mutations", async () => {
        const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ version: 1, data: {} }));
        vi.stubGlobal("fetch", fetchMock);
        vi.stubGlobal("crypto", { randomUUID: () => "intent-key" });

        await api.createActor("csrf-proof", "alice", "Alice");

        const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
        const headers = new Headers(init.headers);
        expect(url).toBe("/api/v1/actors");
        expect(headers.get("X-CSRF-Token")).toBe("csrf-proof");
        expect(headers.get("Idempotency-Key")).toBe("intent-key");
        expect(JSON.parse(String(init.body))).toEqual({ name: "Alice", type: "Person", username: "alice" });
    });

    it("reuses an idempotency key after an ambiguous network failure", async () => {
        const fetchMock = vi
            .fn()
            .mockRejectedValueOnce(new TypeError("network lost"))
            .mockResolvedValueOnce(jsonResponse({ version: 1, data: {} }));
        vi.stubGlobal("fetch", fetchMock);
        vi.stubGlobal("crypto", { randomUUID: vi.fn().mockReturnValueOnce("stable-intent-key") });

        await expect(api.createActor("csrf", "alice", "Alice")).rejects.toThrow("network lost");
        await api.createActor("csrf", "alice", "Alice");

        const first = new Headers((fetchMock.mock.calls[0][1] as RequestInit).headers).get("Idempotency-Key");
        const second = new Headers((fetchMock.mock.calls[1][1] as RequestInit).headers).get("Idempotency-Key");
        expect(first).toBe("stable-intent-key");
        expect(second).toBe(first);
    });

    it("preserves structured backend errors", async () => {
        vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ error: { code: "not_owned", message: "Actor is not owned" } }, 403)));

        const error = await api.actors().catch((reason: unknown) => reason);
        expect(error).toBeInstanceOf(ApiError);
        expect(error).toMatchObject({ code: "not_owned", message: "Actor is not owned", status: 403 });
    });

    it("announces session loss after an authenticated request expires", async () => {
        vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ error: { code: "unauthenticated", message: "authentication required" } }, 401)));
        const listener = vi.fn();
        window.addEventListener("salvia:session-lost", listener, { once: true });

        await expect(api.session()).rejects.toMatchObject({ status: 401 });

        expect(listener).toHaveBeenCalledOnce();
    });

    it("uploads the untouched original and Canvas thumbnail as multipart data", async () => {
        const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ version: 1, data: { id: "media-1", url: "/media/1", preview_url: "/media/thumb" } }));
        vi.stubGlobal("fetch", fetchMock);
        const file = new File(["original"], "photo.png", { type: "image/png" });
        const thumbnail = { blob: new Blob(["thumbnail"], { type: "image/webp" }), originalHeight: 800, originalWidth: 1600 };

        await api.uploadImage("csrf", "actor-1", file, thumbnail, "upload-intent-123456");

        const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
        const form = init.body as FormData;
        expect(new Headers(init.headers).has("Content-Type")).toBe(false);
        expect(form.get("file")).toMatchObject({ name: "photo.png", size: file.size, type: "image/png" });
        expect(form.get("thumbnail")).toBeInstanceOf(File);
        expect(form.get("width")).toBe("1600");
        expect(new Headers(init.headers).get("Idempotency-Key")).toBe("upload-intent-123456");
    });
});
