import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import type { PostImageJob, PostImageResult } from "@/lib/postImage";

const scope = globalThis as unknown as { onmessage?: (event: { data: PostImageJob }) => Promise<void> };

describe("post image worker", () => {
    const close = vi.fn();
    const drawImage = vi.fn();
    const convertToBlob = vi.fn();
    const postMessage = vi.fn();
    beforeEach(async () => {
        vi.resetModules();
        vi.stubGlobal("createImageBitmap", vi.fn().mockResolvedValue({ width: 8000, height: 4000, close }));
        vi.stubGlobal(
            "OffscreenCanvas",
            class {
                getContext = () => ({ drawImage });
                convertToBlob = convertToBlob;
            },
        );
        vi.stubGlobal("postMessage", postMessage);
        convertToBlob.mockResolvedValue(new Blob(["freshly encoded"], { type: "image/webp" }));
        await import("@/lib/postImage.worker");
    });
    afterEach(() => {
        delete scope.onmessage;
        vi.unstubAllGlobals();
        vi.clearAllMocks();
    });
    async function run(allowJPEG = false, type = "image/jpeg") {
        await scope.onmessage?.({ data: { file: new File(["EXIF GPS original"], "private.jpg", { type }), allowJPEG } });
    }
    it("redraws full-image pixels and a thumbnail with bounded size and aspect ratio", async () => {
        await run();
        expect(drawImage.mock.calls).toEqual([
            [expect.anything(), 0, 0, 4096, 2048],
            [expect.anything(), 0, 0, 512, 256],
        ]);
        expect(convertToBlob.mock.calls).toEqual([[{ type: "image/webp", quality: 0.85 }], [{ type: "image/webp", quality: 0.86 }]]);
        const result = postMessage.mock.calls[0][0] as PostImageResult;
        expect(result).toMatchObject({ width: 4096, height: 2048, thumbnail: { width: 512, height: 256 } });
        expect(result.blob.type).toBe("image/webp");
        expect(close).toHaveBeenCalledOnce();
    });
    it("re-encodes already-WebP input instead of passing through original bytes", async () => {
        await run(false, "image/webp");
        expect(drawImage).toHaveBeenCalledTimes(2);
        expect(convertToBlob).toHaveBeenCalledTimes(2);
        expect(postMessage.mock.calls[0][0].blob).toBeInstanceOf(Blob);
    });
    it("allows Safari JPEG fallback for both uploaded image and thumbnail", async () => {
        convertToBlob
            .mockResolvedValueOnce(new Blob(["png"], { type: "image/png" }))
            .mockResolvedValueOnce(new Blob(["jpeg"], { type: "image/jpeg" }))
            .mockResolvedValueOnce(new Blob(["png"], { type: "image/png" }))
            .mockResolvedValueOnce(new Blob(["jpeg"], { type: "image/jpeg" }));
        await run(true);
        const result = postMessage.mock.calls[0][0] as PostImageResult;
        expect(result.blob.type).toBe("image/jpeg");
        expect(result.thumbnail.blob.type).toBe("image/jpeg");
    });
    it("fails closed and releases the bitmap when WebP is unsupported outside Safari", async () => {
        convertToBlob.mockResolvedValueOnce(new Blob(["png"], { type: "image/png" }));
        await run();
        expect(postMessage).toHaveBeenCalledWith({ error: "このブラウザーはWebPエンコードに対応していません" });
        expect(close).toHaveBeenCalledOnce();
    });
    it("does not upscale small images", async () => {
        vi.mocked(createImageBitmap).mockResolvedValue({ width: 100, height: 50, close } as unknown as ImageBitmap);
        await run();
        expect(drawImage.mock.calls).toEqual([
            [expect.anything(), 0, 0, 100, 50],
            [expect.anything(), 0, 0, 100, 50],
        ]);
    });
});
