import { afterEach, describe, expect, it } from "vitest";

import { __resetCssForTests, css, globalCss, keyframes } from "./css";

afterEach(__resetCssForTests);

const output = () => document.head.querySelector("style[data-salvia-css]")?.textContent ?? "";

describe("css", () => {
    it("generates deterministic class names and deduplicates rules", () => {
        const first = css({ color: "red", backgroundColor: "white", width: 10, lineHeight: 1.5 });
        const second = css({ lineHeight: 1.5, width: 10, backgroundColor: "white", color: "red" });

        expect(second).toBe(first);
        expect(output().match(new RegExp(`\\.${first}\\{`, "g"))).toHaveLength(1);
        expect(output()).toContain("background-color:white;");
        expect(output()).toContain("line-height:1.5;");
        expect(output()).toContain("width:10px;");
    });

    it("uses the DOM registry to avoid duplicates after a module reload", () => {
        const className = css({ color: "red" });
        const registry = document.head.querySelector("style[data-salvia-css]");
        const before = output();

        __resetCssForTests();
        document.head.append(registry as HTMLStyleElement);
        css({ color: "red" });

        expect(className).toMatch(/^s-/);
        expect(output()).toBe(before);
    });

    it("serializes nested selectors and conditional rules", () => {
        const className = css({
            "&:hover": { color: "red" },
            "& > svg": { width: "1rem" },
            "@media (width >= 40rem)": { display: "grid" },
            "@supports (display: grid)": { display: "grid" },
        });

        expect(output()).toContain(`.${className}:hover{color:red;}`);
        expect(output()).toContain(`.${className} > svg{width:1rem;}`);
        expect(output()).toContain(`@media (width >= 40rem){.${className}{display:grid;}}`);
        expect(output()).toContain(`@supports (display: grid){.${className}{display:grid;}}`);
    });

    it("registers global rules and keyframes", () => {
        globalCss({ ":root": { "--accent": "#f4bd36" } });
        const animation = keyframes({ from: { opacity: 0 }, to: { opacity: 1 } });

        expect(output()).toContain(":root{--accent:#f4bd36;}");
        expect(output()).toContain(`@keyframes ${animation}{from{opacity:0;}to{opacity:1;}}`);
    });

    it("rejects unsafe properties, selectors, values, and keyframe steps", () => {
        expect(() => css({ "color:red": "blue" })).toThrow("Invalid CSS property");
        expect(() => css({ "&{}": { color: "red" } })).toThrow("Unsafe CSS selector");
        expect(() => css({ color: "red}</style>" })).toThrow("Unsafe CSS value");
        expect(() => css({ color: "red;background:black" })).toThrow("Unsafe CSS value");
        expect(() => css({ "@media print; @import url(example)": { color: "red" } })).toThrow("Invalid CSS at-rule");
        expect(() => keyframes({ middle: { opacity: 1 } })).toThrow("Invalid keyframe step");
    });
});
