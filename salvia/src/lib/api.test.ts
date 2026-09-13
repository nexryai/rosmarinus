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

    it("prepares an object-storage upload with validated metadata", async () => {
        const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ version: 1, data: { id: "media-1", url: "https://objects.test/one", state: "pending", upload_url: "https://s3.test/one", upload_headers: { "Content-Type": "image/png" }, expires_at: "2026-09-14T00:00:00Z" } }));
        vi.stubGlobal("fetch", fetchMock);

        await api.prepareMediaUpload("csrf", "actor-1", { name: "photo.png", content_type: "image/png", size: 8, sha256: "a".repeat(64), width: 1600, height: 800 }, "upload-intent-123456");

        const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
        expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
        expect(JSON.parse(String(init.body))).toMatchObject({ name: "photo.png", width: 1600, sha256: "a".repeat(64) });
        expect(new Headers(init.headers).get("Idempotency-Key")).toBe("upload-intent-123456");
    });

    it("loads management metadata for observed remote emojis", async () => {
        const fetchMock = vi.fn().mockResolvedValue(
            jsonResponse({
                version: 1,
                data: [{ id: "remote-1", host: "remote.test", name: "party", uri: "https://remote.test/emojis/party", url: "https://remote.test/party.webp", original_url: "https://remote.test/party.webp" }],
                next: "cursor",
            }),
        );
        vi.stubGlobal("fetch", fetchMock);

        const result = await api.emojiCatalog("remote", { query: "party", host: "remote.test" });

        expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/emojis?scope=remote&query=party&host=remote.test&limit=30");
        expect(result).toMatchObject({ data: [{ id: "remote-1", host: "remote.test", name: "party" }], next: "cursor" });
    });

    it("sends the selected Actor when importing an observed emoji", async () => {
        const fetchMock = vi.fn().mockResolvedValue(jsonResponse({ version: 1, data: { id: "local-1", host: "", name: "party_here", url: "https://local.test/media/1" } }, 201));
        vi.stubGlobal("fetch", fetchMock);

        await api.importEmoji("csrf", "actor-1", "remote-1", "party_here");

        const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
        expect(url).toBe("/api/v1/emojis/import");
        expect(new Headers(init.headers).get("X-CSRF-Token")).toBe("csrf");
        expect(JSON.parse(String(init.body))).toEqual({ actor_id: "actor-1", source_id: "remote-1", name: "party_here" });
    });
});
