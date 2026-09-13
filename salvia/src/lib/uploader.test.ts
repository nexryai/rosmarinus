import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "./api";
import { uploadImage } from "./uploader";

describe("object-storage uploader", () => {
    afterEach(() => vi.restoreAllMocks());

    it("hashes, uploads, and completes an image through the shared flow", async () => {
        const prepare = vi.spyOn(api, "prepareMediaUpload").mockResolvedValue({
            id: "media-1",
            url: "https://objects.test/object-1",
            state: "pending",
            upload_url: "https://s3.test/object-1",
            upload_headers: { "Content-Type": "image/png", "X-Amz-Meta-Sha256": "digest" },
            expires_at: "2026-09-14T00:00:00Z",
        });
        const complete = vi.spyOn(api, "completeMediaUpload").mockResolvedValue({ id: "media-1", url: "https://objects.test/object-1" });
        const directPut = vi.fn().mockResolvedValue(new Response(null, { status: 200 }));
        vi.stubGlobal("fetch", directPut);
        const file = new File(["image"], "photo.png", { type: "image/png" });

        const result = await uploadImage("csrf", "actor-1", file, { width: 10, height: 20 }, "upload-request-123456");

        expect(prepare).toHaveBeenCalledWith("csrf", "actor-1", expect.objectContaining({ name: "photo.png", size: file.size, width: 10, height: 20, sha256: expect.stringMatching(/^[a-f0-9]{64}$/) }), "upload-request-123456");
        expect(directPut).toHaveBeenCalledWith("https://s3.test/object-1", expect.objectContaining({ method: "PUT", body: file }));
        expect(complete).toHaveBeenCalledWith("csrf", "actor-1", "media-1");
        expect(result).toEqual({ id: "media-1", url: "https://objects.test/object-1", preview_url: "https://objects.test/object-1" });
    });

    it("deletes reserved media when direct upload fails", async () => {
        vi.spyOn(api, "prepareMediaUpload").mockResolvedValue({ id: "media-1", url: "https://objects.test/object-1", state: "pending", upload_url: "https://s3.test/object-1", upload_headers: {}, expires_at: "2026-09-14T00:00:00Z" });
        const cleanup = vi.spyOn(api, "deleteMedia").mockResolvedValue();
        vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(null, { status: 403 })));

        await expect(uploadImage("csrf", "actor-1", new File(["image"], "photo.png", { type: "image/png" }), { width: 10, height: 20 })).rejects.toThrow("403");
        expect(cleanup).toHaveBeenCalledWith("csrf", "actor-1", "media-1");
    });
});
