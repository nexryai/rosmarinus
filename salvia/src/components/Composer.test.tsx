import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Actor, Note } from "../lib/schema";
import { Composer } from "./Composer";

const actor = { id: "alice", username: "alice", name: "Alice", uri: "https://example.test/users/alice" } as Actor;
const target = { id: "note-1", uri: "https://example.test/notes/1", text: "hello", visibility: "public", author: actor } as Note;

describe("Composer", () => {
    beforeEach(() => vi.spyOn(api, "emojis").mockResolvedValue([]));
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("submits a reply with the canonical target URI", async () => {
        const user = userEvent.setup();
        const submit = vi.fn().mockResolvedValue(undefined);
        render(<Composer actor={actor} intent={{ kind: "reply", target }} onClose={() => undefined} onSubmit={submit} />);

        await user.type(screen.getByLabelText("ノート本文"), "返信です");
        await user.click(screen.getByRole("button", { name: "投稿する" }));

        expect(submit).toHaveBeenCalledWith(expect.objectContaining({ in_reply_to_uri: target.uri, text: "返信です", visibility: "public" }));
    });

    it("requires two populated choices for a poll-only post", async () => {
        const user = userEvent.setup();
        const submit = vi.fn().mockResolvedValue(undefined);
        render(<Composer actor={actor} intent={{ kind: "post" }} onClose={() => undefined} onSubmit={submit} />);

        await user.click(screen.getByRole("button", { name: "投票" }));
        expect(screen.getByRole("button", { name: "投稿する" })).toBeDisabled();
        await user.type(screen.getByLabelText("選択肢 1"), "A");
        await user.type(screen.getByLabelText("選択肢 2"), "B");
        await user.click(screen.getByRole("button", { name: "投稿する" }));

        expect(submit).toHaveBeenCalledWith(expect.objectContaining({ poll: { choices: ["A", "B"], multiple: false } }));
    });
});
