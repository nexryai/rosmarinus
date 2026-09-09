import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { Twemoji } from "./Twemoji";

describe("Twemoji", () => {
    afterEach(cleanup);

    it("renders Unicode emoji with pinned jdecked assets while preserving text", () => {
        const { container } = render(<Twemoji text="前👩‍💻後" />);

        expect(container).toHaveTextContent("前後");
        expect(screen.getByAltText("👩‍💻")).toHaveAttribute("src", "https://cdn.jsdelivr.net/gh/jdecked/twemoji@v17.0.3/assets/svg/1f469-200d-1f4bb.svg");
    });

    it("leaves strings without emoji as text", () => {
        const { container } = render(<Twemoji text="Rosemary" />);

        expect(container).toHaveTextContent("Rosemary");
        expect(container.querySelector("img")).toBeNull();
    });
});
