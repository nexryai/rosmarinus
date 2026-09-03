import { afterEach, describe, expect, it, vi } from "vitest";

import { createCanvasThumbnail, revokeCanvasThumbnail } from "./image";

describe("Canvas image processing", () => {
    afterEach(() => vi.restoreAllMocks());

    it("rejects non-image files before opening Canvas", async () => {
        await expect(createCanvasThumbnail(new File(["text"], "note.txt", { type: "text/plain" }))).rejects.toThrow("画像ファイル");
    });

    it("creates a bounded WebP thumbnail in the browser", async () => {
        const close = vi.fn();
        const drawImage = vi.fn();
        vi.stubGlobal("createImageBitmap", vi.fn().mockResolvedValue({ close, height: 800, width: 1600 }));
        vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue({ drawImage } as unknown as CanvasRenderingContext2D);
        vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation((callback, type) => callback(new Blob(["thumbnail"], { type: type || "image/webp" })));
        vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:preview");
        const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);

        const thumbnail = await createCanvasThumbnail(new File(["image"], "photo.jpg", { type: "image/jpeg" }), 512);

        expect(thumbnail).toMatchObject({ height: 256, url: "blob:preview", width: 512 });
        expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 512, 256);
        expect(close).toHaveBeenCalledOnce();
        revokeCanvasThumbnail(thumbnail);
        expect(revoke).toHaveBeenCalledWith("blob:preview");
    });
});
