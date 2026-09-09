import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { EmojiText } from "./EmojiText";
import { Mfm } from "./Mfm";

const emojis = [{ name: "party", url: "https://remote.test/party.webp", media_type: "image/webp" }];

describe("federated text rendering", () => {
    afterEach(cleanup);

    it("renders MFM structure and its supplied custom emoji without HTML injection", () => {
        const { container } = render(<Mfm emojis={emojis} text={'**太字😀** ~~取消~~ [安全](https://remote.test/path) :party: $[ruby 漢字 かんじ] <script>alert("x")</script>'} />);

        expect(screen.getByText("太字").tagName).toBe("STRONG");
        expect(screen.getByAltText("😀")).toHaveAttribute("src", "https://cdn.jsdelivr.net/gh/jdecked/twemoji@v17.0.3/assets/svg/1f600.svg");
        expect(screen.getByText("取消").tagName).toBe("S");
        expect(screen.getByRole("link", { name: "安全" })).toHaveAttribute("href", "https://remote.test/path");
        expect(screen.getByAltText(":party:")).toHaveAttribute("src", emojis[0].url);
        expect(screen.getByText("かんじ").tagName).toBe("RT");
        expect(container.querySelector("script")).toBeNull();
    });

    it("renders name emoji but deliberately leaves MFM syntax literal", () => {
        const { container } = render(<EmojiText emojis={emojis} text="**Alice😀** :party:" />);

        expect(container).toHaveTextContent("**Alice**");
        expect(container.querySelector("strong")).toBeNull();
        expect(screen.getByAltText("😀")).toHaveAttribute("src", "https://cdn.jsdelivr.net/gh/jdecked/twemoji@v17.0.3/assets/svg/1f600.svg");
        expect(screen.getByAltText(":party:")).toHaveAttribute("src", emojis[0].url);
    });

    it("keeps unsupported functions visible and tolerates an invalid unixtime", () => {
        const { container } = render(<Mfm text="$[future syntax] $[unixtime 999999999999999999999]" />);

        expect(container).toHaveTextContent("$[future syntax]");
        expect(container).toHaveTextContent("$[unixtime 999999999999999999999]");
    });
});
