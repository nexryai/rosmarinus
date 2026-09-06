import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Actor, Profile } from "../lib/schema";
import { ProfilePage } from "./ProfilePage";

const remote = { id: "bob", username: "bob", name: "Bob", uri: "https://remote.test/users/bob", profile_fields: [], tags: [] } as unknown as Actor;
const profile = { actor: remote, followers_count: 2, following_count: 3, follow_status: "", blocked_by_viewer: false } as Profile;

describe("ProfilePage social actions", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("follows, blocks, and reverses a block using server relationship state", async () => {
        vi.spyOn(api, "profile").mockResolvedValue(profile);
        const follow = vi.spyOn(api, "follow").mockResolvedValue(undefined);
        const unfollow = vi.spyOn(api, "unfollow").mockResolvedValue(undefined);
        const block = vi.spyOn(api, "block").mockResolvedValue(undefined);
        const unblock = vi.spyOn(api, "unblock").mockResolvedValue(undefined);
        vi.spyOn(window, "confirm").mockReturnValue(true);
        const user = userEvent.setup();
        render(<ProfilePage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} profileID="bob" />);
        await screen.findByRole("heading", { name: "Bob" });
        expect(screen.getByText("@bob@remote.test")).toBeInTheDocument();

        await user.click(screen.getByRole("button", { name: "フォロー" }));
        await user.click(screen.getByRole("button", { name: "フォロー解除" }));
        await user.click(screen.getByRole("button", { name: "ブロック" }));
        await user.click(screen.getByRole("button", { name: "ブロック解除" }));

        expect(follow).toHaveBeenCalledWith("csrf", "alice", remote.uri);
        expect(unfollow).toHaveBeenCalledWith("csrf", "alice", remote.uri);
        expect(block).toHaveBeenCalledWith("csrf", "alice", remote.uri);
        expect(unblock).toHaveBeenCalledWith("csrf", "alice", remote.uri);
    });

    it("opens a server-filtered follower list", async () => {
        vi.spyOn(api, "profile").mockResolvedValue(profile);
        vi.spyOn(api, "profileConnections").mockResolvedValue([{ id: "follow-1", status: "accepted", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote }]);
        const user = userEvent.setup();
        render(<ProfilePage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} profileID="bob" />);
        await screen.findByRole("heading", { name: "Bob" });
        await user.click(screen.getByRole("button", { name: "2フォロワー" }));

        await waitFor(() => expect(api.profileConnections).toHaveBeenCalledWith("alice", "bob", "followers"));
        expect(screen.getByRole("dialog", { name: "フォロワー" })).toBeInTheDocument();
    });
});
