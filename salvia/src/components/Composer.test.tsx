import { cleanup, fireEvent, render, screen } from "@testing-library/react";
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
        delete document.documentElement.dataset.reduceMotion;
    });

    it("submits a reply with the canonical target URI", async () => {
        const user = userEvent.setup();
        const submit = vi.fn().mockResolvedValue(undefined);
        render(<Composer actor={actor} csrf="csrf" intent={{ kind: "reply", target }} onClose={() => undefined} onSubmit={submit} />);

        await user.type(screen.getByLabelText("ノート本文"), "返信です");
        await user.click(screen.getByRole("button", { name: "投稿する" }));

        expect(submit).toHaveBeenCalledWith(expect.objectContaining({ in_reply_to_uri: target.uri, text: "返信です", visibility: "public" }), expect.any(String));
    });

    it("requires two populated choices for a poll-only post", async () => {
        const user = userEvent.setup();
        const submit = vi.fn().mockResolvedValue(undefined);
        render(<Composer actor={actor} csrf="csrf" intent={{ kind: "post" }} onClose={() => undefined} onSubmit={submit} />);

        await user.click(screen.getByRole("button", { name: "投票" }));
        expect(screen.getByRole("button", { name: "投稿する" })).toBeDisabled();
        await user.type(screen.getByLabelText("選択肢 1"), "A");
        await user.type(screen.getByLabelText("選択肢 2"), "B");
        await user.click(screen.getByRole("button", { name: "投稿する" }));

        expect(submit).toHaveBeenCalledWith(expect.objectContaining({ poll: { choices: ["A", "B"], multiple: false } }), expect.any(String));
    });

    it("inserts a custom emoji chosen from the shared picker", async () => {
        vi.spyOn(api, "emojis").mockResolvedValue([{ name: "salvia", url: "/media/salvia", media_type: "image/webp" }]);
        const user = userEvent.setup();
        const submit = vi.fn().mockResolvedValue(undefined);
        render(<Composer actor={actor} csrf="csrf" intent={{ kind: "post" }} onClose={() => undefined} onSubmit={submit} />);

        await user.type(screen.getByLabelText("ノート本文"), "咲いた ");
        await user.click(screen.getByRole("button", { name: "絵文字" }));
        await user.type(screen.getByLabelText("絵文字を検索"), "salvia");
        await user.click(await screen.findByRole("button", { name: ":salvia:" }));
        await user.click(screen.getByRole("button", { name: "投稿する" }));

        expect(submit).toHaveBeenCalledWith(expect.objectContaining({ text: "咲いた :salvia:", emoji_names: ["salvia"] }), expect.any(String));
    });

    it("keeps icon-only composer controls labeled for assistive technology", () => {
        render(<Composer actor={actor} csrf="csrf" intent={{ kind: "post" }} onClose={() => undefined} onSubmit={vi.fn()} />);

        expect(screen.getByRole("button", { name: "CW" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "投票" })).toBeInTheDocument();
        expect(screen.getByRole("button", { name: "絵文字" })).toBeInTheDocument();
        expect(screen.getByLabelText("画像")).toHaveAttribute("type", "file");
    });

    it("dismisses only the emoji picker when Escape is pressed", async () => {
        document.documentElement.dataset.reduceMotion = "true";
        const user = userEvent.setup();
        const onClose = vi.fn();
        render(<Composer actor={actor} csrf="csrf" intent={{ kind: "post" }} onClose={onClose} onSubmit={vi.fn()} />);

        await user.click(screen.getByRole("button", { name: "絵文字" }));
        expect(screen.getByRole("dialog", { name: "絵文字を選択" })).toBeInTheDocument();

        fireEvent.keyDown(document, { key: "Escape" });

        expect(screen.queryByRole("dialog", { name: "絵文字を選択" })).not.toBeInTheDocument();
        expect(screen.getByRole("dialog", { name: "新しいノート" })).toBeInTheDocument();
        expect(onClose).not.toHaveBeenCalled();
    });
});
