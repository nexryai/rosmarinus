import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { EmojiText } from "./EmojiText";
import { Mfm } from "./Mfm";

const emojis = [{ name: "party", url: "https://remote.test/party.webp", media_type: "image/webp" }];

describe("federated text rendering", () => {
    afterEach(cleanup);

    it("renders MFM structure and its supplied custom emoji without HTML injection", () => {
        const { container } = render(<Mfm emojis={emojis} text={'**太字😀** ~~取消~~ [安全](https://remote.test/path) :party: $[ruby 漢字 かんじ] <script>alert("x")</script>'} />);

        expect(screen.getByText("太字😀").tagName).toBe("STRONG");
        expect(screen.getByText("取消").tagName).toBe("S");
        expect(screen.getByRole("link", { name: "安全" })).toHaveAttribute("href", "https://remote.test/path");
        const emoji = screen.getByAltText(":party:");
        expect(emoji).toHaveAttribute("src", emojis[0].url);
        expect(emoji.style.width).toBe("auto");
        expect(emoji.style.height).toBe("2em");
        expect(emoji.style.verticalAlign).toBe("middle");
        expect(screen.getByText("かんじ").tagName).toBe("RT");
        expect(container.querySelector("script")).toBeNull();
    });

    it("renders name emoji but deliberately leaves MFM syntax literal", () => {
        const { container } = render(<EmojiText emojis={emojis} text="**Alice😀** :party:" />);

        expect(container).toHaveTextContent("**Alice😀**");
        expect(container.querySelector("strong")).toBeNull();
        const emoji = screen.getByAltText(":party:");
        expect(emoji).toHaveAttribute("src", emojis[0].url);
        expect(emoji.style.width).toBe("auto");
        expect(emoji.style.height).toBe("1.25em");
        expect(emoji.style.verticalAlign).toBe("-0.25em");
    });

    it("keeps unsupported functions visible and tolerates an invalid unixtime", () => {
        const { container } = render(<Mfm text="$[future syntax] $[unixtime 999999999999999999999]" />);

        expect(container).toHaveTextContent("$[future syntax]");
        expect(container).toHaveTextContent("$[unixtime 999999999999999999999]");
    });

    it("renders local and remote mentions with one leading at-sign", () => {
        const { container } = render(<Mfm text="@alice から @bob@remote.test への返信" />);

        expect(screen.getByText("@alice")).toBeInTheDocument();
        expect(screen.getByText("@bob@remote.test")).toBeInTheDocument();
        expect(container).not.toHaveTextContent("@@alice");
        expect(container).not.toHaveTextContent("@@bob@remote.test");
    });

    it("renders resolved local and remote mentions as linked avatar pills", () => {
        const onOpenProfile = vi.fn();
        render(
            <Mfm
                mentions={[
                    { id: "actor-1", username: "alice", name: "Alice", host: "", avatar_url: "/media/alice", uri: "https://example.test/users/alice", emojis: [] },
                    { id: "actor-2", username: "bob", name: "Bob", host: "remote.test", avatar_url: "/media/bob", uri: "https://remote.test/users/bob", emojis: [] },
                ]}
                onOpenProfile={onOpenProfile}
                text="@alice と @bob@remote.test と @carol へ"
            />,
        );

        const alice = screen.getByRole("link", { name: "@alice" });
        expect(alice).toHaveAttribute("href", "/profiles/actor-1");
        expect(alice.querySelector("img")).toHaveAttribute("src", "/media/alice");
        const bob = screen.getByRole("link", { name: "@bob@remote.test" });
        expect(bob).toHaveAttribute("href", "/profiles/actor-2");
        expect(screen.getByText("@carol")).toBeInTheDocument();

        fireEvent.click(alice);
        expect(onOpenProfile).toHaveBeenCalledWith("actor-1");
    });

    it("keeps unresolved mentions as plain text", () => {
        const { container } = render(<Mfm text="@ghost@remote.test へ" />);

        expect(screen.getByText("@ghost@remote.test")).toBeInTheDocument();
        expect(container.querySelector("a")).toBeNull();
    });

    it("keeps numbers in code on a text font before falling back to Twemoji", () => {
        render(<Mfm text="`123😀`" />);

        const code = screen.getByText("123😀");
        expect(code.style.fontFamily).toBe('SFMono-Regular, Consolas, "Liberation Mono", "DejaVu Sans Mono", "Jdecked Twemoji", "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji", ui-monospace, monospace');
    });

    it("nyaizes text nodes without changing URLs, mentions, hashtags, or code", () => {
        render(<Mfm nyaize text="な morning everyone https://example.test/nana @nana #な `な`" />);

        expect(screen.getByText(/にゃ mornyan everynyan/)).toBeInTheDocument();
        expect(screen.getByRole("link", { name: "https://example.test/nana" })).toHaveAttribute("href", "https://example.test/nana");
        expect(screen.getByText("@nana")).toBeInTheDocument();
        expect(screen.getByText("#な")).toBeInTheDocument();
        expect(screen.getByText("な", { selector: "code" })).toBeInTheDocument();
    });
});
