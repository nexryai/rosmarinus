import { afterEach, describe, expect, it, vi } from "vitest";

import { preparePostImage } from "@/lib/postImage";

describe("post image upload boundary", () => {
    afterEach(() => {
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });
    const original = new File(["EXIF GPS original bytes"], "private-location.jpg", { type: "image/jpeg", lastModified: 1234 });
    function worker(type = "image/webp") {
        vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:preview");
        vi.stubGlobal(
            "Worker",
            class {
                onmessage?: (event: { data: unknown }) => void;
                terminate = vi.fn();
                postMessage() {
                    this.onmessage?.({ data: { blob: new Blob(["encoded bytes"], { type }), width: 100, height: 50, thumbnail: { blob: new Blob(["preview"], { type }), width: 100, height: 50 } } });
                }
            },
        );
    }
    it("creates a new upload file without source bytes, name, or timestamp", async () => {
        worker();
        const result = await preparePostImage(original, new AbortController().signal);
        expect(result.file).not.toBe(original);
        expect(result.file.name).toBe("image.webp");
        expect(result.file.type).toBe("image/webp");
        expect(result.file.lastModified).toBe(0);
        expect(await result.file.text()).toBe("encoded bytes");
        expect(result).toMatchObject({ width: 100, height: 50, thumbnailURL: "blob:preview" });
    });
    it("permits JPEG only when the user agent is Safari", async () => {
        worker("image/jpeg");
        await expect(preparePostImage(original, new AbortController().signal)).rejects.toThrow("安全にエンコード");
        vi.spyOn(navigator, "userAgent", "get").mockReturnValue("Mozilla/5.0 Version/18.0 Safari/605.1.15");
        expect((await preparePostImage(original, new AbortController().signal)).file.name).toBe("image.jpg");
    });
    it("rejects an unsupported encoder instead of falling back to the original", async () => {
        vi.stubGlobal("Worker", undefined);
        await expect(preparePostImage(original, new AbortController().signal)).rejects.toThrow("対応したブラウザー");
    });
    it("rejects non-images before starting a worker", async () => {
        const spawn = vi.fn();
        vi.stubGlobal("Worker", spawn);
        await expect(preparePostImage(new File(["text"], "note.txt", { type: "text/plain" }), new AbortController().signal)).rejects.toThrow("画像ファイル");
        expect(spawn).not.toHaveBeenCalled();
    });
});
