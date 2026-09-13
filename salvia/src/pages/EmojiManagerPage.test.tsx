import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { ManagedEmoji } from "../lib/schema";
import { EmojiManagerPage } from "./EmojiManagerPage";

const localEmoji = { id: "local-1", host: "", name: "rosemary", uri: "https://local.test/emojis/rosemary", url: "https://local.test/media/1", original_url: "https://local.test/media/1" } as ManagedEmoji;
const remoteEmoji = { id: "remote-1", host: "remote.test", name: "party", uri: "https://remote.test/emojis/party", url: "https://remote.test/party.webp", original_url: "https://remote.test/party.webp" } as ManagedEmoji;

describe("custom emoji manager", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("edits and deletes local custom emojis with confirmation", async () => {
        vi.spyOn(api, "emojiCatalog").mockResolvedValue({ data: [localEmoji], next: "" });
        const update = vi.spyOn(api, "updateEmoji").mockResolvedValue({ ...localEmoji, name: "herb" });
        const remove = vi.spyOn(api, "deleteEmoji").mockResolvedValue(undefined);
        const changed = vi.fn();
        const user = userEvent.setup();
        render(<EmojiManagerPage actorID="actor-1" csrf="csrf" onCatalogChanged={changed} />);

        expect(await screen.findByText(":rosemary:")).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "編集" }));
        const editor = screen.getByRole("dialog", { name: "カスタム絵文字を編集" });
        const name = within(editor).getByLabelText("名前");
        await user.clear(name);
        await user.type(name, "herb");
        await user.click(within(editor).getByRole("button", { name: "保存" }));

        await waitFor(() => expect(update).toHaveBeenCalledWith("csrf", "actor-1", "local-1", "herb", ""));
        expect(await screen.findByText(":herb:")).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "削除" }));
        const confirmation = screen.getByRole("dialog", { name: "カスタム絵文字を削除しますか？" });
        expect(remove).not.toHaveBeenCalled();
        await user.click(within(confirmation).getByRole("button", { name: "削除" }));

        await waitFor(() => expect(remove).toHaveBeenCalledWith("csrf", "local-1"));
        expect(screen.queryByText(":herb:")).not.toBeInTheDocument();
        expect(changed).toHaveBeenCalledTimes(2);
    });

    it("browses observed remote emojis and imports one under a chosen name", async () => {
        vi.spyOn(api, "emojiCatalog").mockImplementation(async (scope) => ({ data: scope === "remote" ? [remoteEmoji] : [], next: "" }));
        const imported = vi.spyOn(api, "importEmoji").mockResolvedValue({ ...remoteEmoji, id: "local-imported", host: "", name: "party_here", url: "https://local.test/media/imported" });
        const changed = vi.fn();
        const user = userEvent.setup();
        render(<EmojiManagerPage actorID="actor-1" csrf="csrf" onCatalogChanged={changed} />);

        await user.click(screen.getByRole("tab", { name: "リモート" }));
        expect(await screen.findByText(":party:")).toBeInTheDocument();
        expect(screen.getByText("remote.test")).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "インポート" }));
        const editor = screen.getByRole("dialog", { name: "リモート絵文字をインポート" });
        const name = within(editor).getByLabelText("ローカルでの名前");
        await user.clear(name);
        await user.type(name, "party_here");
        await user.click(within(editor).getByRole("button", { name: "インポート" }));

        await waitFor(() => expect(imported).toHaveBeenCalledWith("csrf", "actor-1", "remote-1", "party_here"));
        expect(changed).toHaveBeenCalledOnce();
    });
});
