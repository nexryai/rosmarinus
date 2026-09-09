import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { AccountSettings, Actor } from "../lib/schema";
import { SettingsPage } from "./SettingsPage";

const actor = {
    id: "alice",
    username: "alice",
    name: "Alice",
    summary: "",
    avatar_url: "",
    uri: "https://example.test/users/alice",
} as Actor;

const accountSettings: AccountSettings = {
    theme: "yellow",
    reduce_motion: false,
    compact_mode: false,
    selected_actor_id: actor.id,
};

describe("SettingsPage destructive actions", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("deletes an Actor only after the custom confirmation dialog", async () => {
        vi.spyOn(api, "actorSettings").mockResolvedValue({
            actor_id: actor.id,
            default_visibility: "public",
            show_content_warning: false,
            display_order: 0,
            pinned: false,
        });
        const deleteActor = vi.spyOn(api, "deleteActor").mockResolvedValue(undefined);
        const onActorsChanged = vi.fn().mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<SettingsPage accountSettings={accountSettings} actors={[actor, { ...actor, id: "bob", username: "bob" }]} csrf="csrf" onActorsChanged={onActorsChanged} onSettingsChanged={vi.fn()} selectedActor={actor} />);

        await user.click(screen.getByRole("button", { name: "このActorを削除" }));
        expect(deleteActor).not.toHaveBeenCalled();
        expect(screen.getByRole("dialog", { name: "Actorを削除しますか？" })).toBeInTheDocument();

        await user.click(screen.getByRole("button", { name: "削除する" }));
        await waitFor(() => expect(deleteActor).toHaveBeenCalledWith("csrf", actor.id));
        expect(onActorsChanged).toHaveBeenCalledOnce();
    });
});
