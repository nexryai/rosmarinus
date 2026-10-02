import { describe, expect, it, vi } from "vitest";

import { encodeCanvas } from "@/lib/canvasEncoding";
import { isSafari } from "@/lib/canvasWorker";

describe("Canvas encoding policy", () => {
    it("falls back to JPEG for Safari when WebP conversion throws", async () => {
        const convertToBlob = vi
            .fn()
            .mockRejectedValueOnce(new Error("Unsupported"))
            .mockResolvedValueOnce(new Blob(["jpeg"], { type: "image/jpeg" }));
        const result = await encodeCanvas({ convertToBlob } as unknown as OffscreenCanvas, true, 0.85);
        expect(result.type).toBe("image/jpeg");
        expect(convertToBlob).toHaveBeenLastCalledWith({ type: "image/jpeg", quality: 0.85 });
    });
    it("never falls back to JPEG after an encoder error outside Safari", async () => {
        const convertToBlob = vi.fn().mockRejectedValue(new Error("Encoding failed"));
        await expect(encodeCanvas({ convertToBlob } as unknown as OffscreenCanvas, false, 0.85)).rejects.toThrow("Encoding failed");
        expect(convertToBlob).toHaveBeenCalledOnce();
    });
    it("does not classify Chrome on iOS as Safari", () => {
        const agent = vi.spyOn(navigator, "userAgent", "get").mockReturnValue("Mozilla/5.0 CriOS/140.0 Mobile/15E148 Safari/604.1");
        expect(isSafari()).toBe(false);
        agent.mockReturnValue("Mozilla/5.0 Version/18.0 Mobile/15E148 Safari/604.1");
        expect(isSafari()).toBe(true);
        agent.mockRestore();
    });
});
