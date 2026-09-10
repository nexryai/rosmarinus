import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { GlobalStyles } from "./globalStyles";
import { __resetCssForTests } from "./lib/css";

afterEach(() => {
    cleanup();
    __resetCssForTests();
});

describe("GlobalStyles", () => {
    it("serves Twemoji fonts locally and applies them to editable controls", () => {
        render(<GlobalStyles />);

        const css = document.head.querySelector("style[data-salvia-css]")?.textContent ?? "";
        expect(css).toContain('@font-face{font-display:swap;font-family:"Jdecked Twemoji"');
        expect(css).toContain("JdeckedTwemoji-SVG.woff2");
        expect(css).toContain("JdeckedTwemoji-COLRv1.woff2");
        expect(css).toContain(":root{--accent:#f4bd36;");
        expect(css).toContain('font-family:Inter, "Noto Sans JP", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Liberation Sans", "DejaVu Sans", "Jdecked Twemoji", "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji", ui-sans-serif, system-ui, sans-serif;');
        expect(css).toContain("button, input, textarea, select{color:inherit;font:inherit;");
        expect(css).not.toContain("https://");
    });
});
