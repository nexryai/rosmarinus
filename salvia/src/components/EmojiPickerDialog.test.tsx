import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { EmojiPickerDialog } from "./EmojiPickerDialog";

const customEmojis = [
    { name: "salvia", url: "/media/salvia", media_type: "image/webp" },
    { name: "rosemary", url: "/media/rosemary", media_type: "image/webp" },
];

describe("EmojiPickerDialog", () => {
    afterEach(cleanup);

    it("shows custom and default Unicode emojis and returns a normalized value", async () => {
        const user = userEvent.setup();
        const onSelect = vi.fn();
        const onClose = vi.fn();
        render(<EmojiPickerDialog emojis={customEmojis} onClose={onClose} onSelect={onSelect} />);

        expect(screen.getByRole("button", { name: ":salvia:" })).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "👍" }));

        expect(onSelect).toHaveBeenCalledWith("👍");
        expect(onClose).toHaveBeenCalledOnce();
    });

    it("filters Unicode emojis by their English emojibase keywords", async () => {
        const user = userEvent.setup();
        const onSelect = vi.fn();
        render(<EmojiPickerDialog emojis={customEmojis} onClose={vi.fn()} onSelect={onSelect} />);

        await user.type(screen.getByLabelText("絵文字を検索"), "party");
        expect(screen.queryByRole("button", { name: ":salvia:" })).not.toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "🎉" }));

        expect(onSelect).toHaveBeenCalledWith("🎉");
    });

    it("filters local custom emojis by name", async () => {
        const user = userEvent.setup();
        const onSelect = vi.fn();
        render(<EmojiPickerDialog emojis={customEmojis} onClose={vi.fn()} onSelect={onSelect} />);

        await user.type(screen.getByLabelText("絵文字を検索"), "rosemary");
        expect(screen.queryByRole("button", { name: ":salvia:" })).not.toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: ":rosemary:" }));

        expect(onSelect).toHaveBeenCalledWith(":rosemary:");
    });

    it("reports when no emoji matches the filter", async () => {
        const user = userEvent.setup();
        render(<EmojiPickerDialog emojis={customEmojis} onClose={vi.fn()} onSelect={vi.fn()} />);

        await user.type(screen.getByLabelText("絵文字を検索"), "zzzzzz");

        expect(screen.getByText("該当する絵文字がありません")).toBeInTheDocument();
    });
});
