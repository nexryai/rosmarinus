import { fireEvent } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { registerServiceWorker } from "./pwa";

describe("PWA registration", () => {
    const originalServiceWorker = Object.getOwnPropertyDescriptor(navigator, "serviceWorker");

    afterEach(() => {
        vi.restoreAllMocks();
        if (originalServiceWorker) Object.defineProperty(navigator, "serviceWorker", originalServiceWorker);
        else Reflect.deleteProperty(navigator, "serviceWorker");
    });

    it("registers the same-origin service worker after the page loads", () => {
        const register = vi.fn().mockResolvedValue(undefined);
        Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { register } });

        registerServiceWorker();
        expect(register).not.toHaveBeenCalled();
        fireEvent.load(window);

        expect(register).toHaveBeenCalledWith("/sw.js", { scope: "/", updateViaCache: "none" });
    });
});
