import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { ImageJob, ImageResult } from "@/lib/profileImage";

const scope = globalThis as unknown as { onmessage?: (event: { data: ImageJob }) => Promise<void> };

describe("profile image worker encoding", () => {
    const close = vi.fn();
    const drawImage = vi.fn();
    const convertToBlob = vi.fn();
    const postMessage = vi.fn();
    beforeEach(async () => {
        vi.resetModules();
        vi.stubGlobal("createImageBitmap", vi.fn().mockResolvedValue({ width: 3000, height: 2000, close }));
        vi.stubGlobal(
            "OffscreenCanvas",
            class {
                getContext = () => ({ drawImage });
                convertToBlob = convertToBlob;
            },
        );
        vi.stubGlobal("postMessage", postMessage);
        convertToBlob.mockResolvedValue(new Blob(["webp"], { type: "image/webp" }));
        await import("@/lib/profileImage.worker");
    });
    afterEach(() => {
        delete scope.onmessage;
        vi.unstubAllGlobals();
        vi.clearAllMocks();
    });
    async function run(allowJPEG = false) {
        await scope.onmessage?.({ data: { file: new File(["original"], "image.png", { type: "image/png" }), crop: { aspect: 3, zoom: 1, x: 0.5, y: 0.5 }, allowJPEG } });
    }
    it("draws the requested source rectangle and exports WebP with bounded dimensions", async () => {
        await run();
        expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 500, 3000, 1000, 0, 0, 2048, 683);
        expect(convertToBlob).toHaveBeenCalledWith({ type: "image/webp", quality: 0.9 });
        const result = postMessage.mock.calls[0][0] as ImageResult;
        expect(result.blob.type).toBe("image/webp");
        expect(result.width).toBe(2048);
        expect(close).toHaveBeenCalledOnce();
    });
    it("allows JPEG fallback only for Safari", async () => {
        convertToBlob.mockResolvedValueOnce(new Blob(["png"], { type: "image/png" })).mockResolvedValueOnce(new Blob(["jpeg"], { type: "image/jpeg" }));
        await run(true);
        expect(convertToBlob).toHaveBeenLastCalledWith({ type: "image/jpeg", quality: 0.9 });
        expect(postMessage.mock.calls[0][0].blob.type).toBe("image/jpeg");
    });
    it("rejects a silent PNG fallback in other browsers", async () => {
        convertToBlob.mockResolvedValueOnce(new Blob(["png"], { type: "image/png" }));
        await run();
        expect(postMessage).toHaveBeenCalledWith({ error: "このブラウザーはWebPエンコードに対応していません" });
        expect(convertToBlob).toHaveBeenCalledOnce();
        expect(close).toHaveBeenCalledOnce();
    });
    it("closes the decoded bitmap even if encoding fails", async () => {
        convertToBlob.mockRejectedValueOnce(new Error("Encoding failed"));
        await run();
        expect(postMessage).toHaveBeenCalledWith({ error: "Encoding failed" });
        expect(close).toHaveBeenCalledOnce();
    });
});
