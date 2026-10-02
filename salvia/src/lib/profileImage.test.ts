import { afterEach, describe, expect, it, vi } from "vitest";

import { cropRectangle, processProfileImage } from "@/lib/profileImage";

describe("profile image processing", () => {
    afterEach(() => vi.unstubAllGlobals());
    it("keeps crop geometry within the image for both aspect ratios and zoom", () => {
        expect(cropRectangle(1200, 800, { aspect: 1, zoom: 1, x: 1, y: 0 })).toEqual({ x: 400, y: 0, width: 800, height: 800 });
        expect(cropRectangle(1200, 800, { aspect: 3, zoom: 2, x: 0.5, y: 1 })).toEqual({ x: 300, y: 600, width: 600, height: 200 });
        expect(() => cropRectangle(100, 100, { aspect: 1, zoom: 0, x: 0.5, y: 0.5 })).toThrow();
    });
    it("sends files to a module worker and terminates on cancellation", async () => {
        const terminate = vi.fn();
        const postMessage = vi.fn();
        vi.stubGlobal(
            "Worker",
            class {
                terminate = terminate;
                postMessage = postMessage;
            },
        );
        const controller = new AbortController();
        const file = new File(["image"], "image.png", { type: "image/png" });
        const result = processProfileImage(file, controller.signal);
        expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({ file, allowJPEG: false }));
        controller.abort();
        await expect(result).rejects.toMatchObject({ name: "AbortError" });
        expect(terminate).toHaveBeenCalledOnce();
    });
});
