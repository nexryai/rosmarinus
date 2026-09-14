import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Actor, Connection, Notification } from "../lib/schema";
import { FollowRequestsPage } from "./FollowRequestsPage";
import { NotificationsPage } from "./NotificationsPage";

const remote = { id: "bob", username: "bob", name: "Bob", uri: "https://remote.test/users/bob" } as Actor;

describe("social inbox mutations", () => {
    beforeEach(() => {
        vi.spyOn(api, "notificationUnreadCount").mockResolvedValue(0);
        vi.spyOn(api, "markAllNotificationsRead").mockResolvedValue(0);
    });

    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it("approves a mandatory follow request and removes it from the queue", async () => {
        const item = { id: "follow-1", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        const decide = vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const onOpenProfile = vi.fn();
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={onOpenProfile} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "Bobのプロフィールを開く" }));
        await user.click(await screen.findByRole("button", { name: "承認" }));

        expect(onOpenProfile).toHaveBeenCalledWith("bob");
        expect(decide).not.toHaveBeenCalled();
        const dialog = screen.getByRole("dialog", { name: "フォローリクエストを承認しますか？" });
        await user.click(within(dialog).getByRole("button", { name: "承認" }));
        expect(decide).toHaveBeenCalledWith("csrf", "alice", "bob", "accepted");
        expect(screen.queryByText("@bob")).not.toBeInTheDocument();
    });

    it("lists sent requests and cancels one from the sent tab", async () => {
        const item = { id: "sent-1", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([]);
        const sent = vi.spyOn(api, "sentFollowRequests").mockResolvedValue([item]);
        const unfollow = vi.spyOn(api, "unfollow").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);

        await user.click(screen.getByRole("tab", { name: "送信したリクエスト" }));
        expect(await screen.findByText("@bob")).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "申請を取り消す" }));

        expect(sent).toHaveBeenCalledWith("alice");
        expect(unfollow).toHaveBeenCalledWith("csrf", "alice", remote.uri);
        expect(screen.queryByText("@bob")).not.toBeInTheDocument();
        expect(screen.getByRole("textbox", { name: "ハンドルまたはプロフィールURL" })).toBeInTheDocument();
    });

    it("approves a request and follows the remote Actor back", async () => {
        const item = { id: "follow-back", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        const decide = vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const follow = vi.spyOn(api, "follow").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "承認してフォローバック" }));
        expect(decide).not.toHaveBeenCalled();
        const dialog = screen.getByRole("dialog", { name: "承認してフォローバックしますか？" });
        await user.click(within(dialog).getByRole("button", { name: "承認してフォローバック" }));

        await waitFor(() => expect(follow).toHaveBeenCalledWith("csrf", "alice", "https://remote.test/users/bob"));
        expect(decide).toHaveBeenCalledWith("csrf", "alice", "bob", "accepted");
        expect(decide.mock.invocationCallOrder[0]).toBeLessThan(follow.mock.invocationCallOrder[0]);
        expect(screen.queryByText("@bob")).not.toBeInTheDocument();
    });

    it("keeps an accepted request removed when only the follow-back fails", async () => {
        const item = { id: "follow-back-failure", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        vi.spyOn(api, "follow").mockRejectedValue(new Error("remote unavailable"));
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "承認してフォローバック" }));
        await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "承認してフォローバック" }));

        expect(await screen.findByRole("alert")).toHaveTextContent("リクエストは承認しましたが、フォローバックに失敗しました: remote unavailable");
        expect(screen.queryByText("@bob")).not.toBeInTheDocument();
    });

    it("marks all Actor notifications read when the page opens", async () => {
        const item = { id: "notification-1", actor_id: "alice", kind: "followRequest", created_at: "2026-01-01T00:00:00Z", is_read: false, read_at: null, source: remote } as Notification;
        vi.spyOn(api, "notifications").mockResolvedValue([item]);
        vi.mocked(api.notificationUnreadCount).mockResolvedValue(1);
        vi.mocked(api.markAllNotificationsRead).mockResolvedValue(1);
        const onOpenProfile = vi.fn();
        const onUnreadCountChange = vi.fn();
        const user = userEvent.setup();
        render(<NotificationsPage actorID="alice" csrf="csrf" onActorChange={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={onOpenProfile} onUnreadCountChange={onUnreadCountChange} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "Bobのプロフィールを開く" }));

        expect(onOpenProfile).toHaveBeenCalledWith("bob");
        expect(api.markAllNotificationsRead).toHaveBeenCalledWith("csrf", "alice");
        expect(onUnreadCountChange).toHaveBeenCalledWith(0);
        expect(screen.queryByRole("button", { name: "既読" })).not.toBeInTheDocument();
    });

    it("shows Misskey-style notification context and opens its note", async () => {
        const item = {
            id: "notification-2",
            actor_id: "alice",
            kind: "renote",
            note_id: "note-1",
            created_at: new Date().toISOString(),
            is_read: true,
            read_at: new Date().toISOString(),
            source: remote,
            note: { id: "note-1", text: "Rosemaryへようこそ" },
        } as Notification;
        vi.spyOn(api, "notifications").mockResolvedValue([item]);
        const onOpenNote = vi.fn();
        const user = userEvent.setup();
        render(<NotificationsPage actorID="alice" csrf="csrf" onActorChange={vi.fn()} onOpenNote={onOpenNote} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByText("がリノートしました")).toBeInTheDocument();
        expect(screen.getByText((_, element) => element?.tagName === "BLOCKQUOTE" && element.textContent === "“Rosemaryへようこそ”")).toBeInTheDocument();
        expect(screen.getByRole("img", { name: "リノートしました" })).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "ノートを開く" }));

        expect(onOpenNote).toHaveBeenCalledWith("note-1");
    });

    it("renders the custom emoji carried by a reaction notification", async () => {
        const item = {
            id: "notification-reaction",
            actor_id: "alice",
            kind: "reaction",
            note_id: "note-1",
            reaction: ":party@remote.test:",
            reaction_emoji: { name: "party", url: "https://remote.test/party.webp", media_type: "image/webp" },
            created_at: new Date().toISOString(),
            is_read: true,
            source: remote,
        } as Notification;
        vi.spyOn(api, "notifications").mockResolvedValue([item]);

        render(<NotificationsPage actorID="alice" csrf="csrf" onActorChange={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByTitle(":party:")).toHaveAttribute("src", "https://remote.test/party.webp");
        expect(screen.queryByText(":party@remote.test:")).not.toBeInTheDocument();
    });

    it("rejects a mandatory follow request", async () => {
        const item = { id: "follow-2", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        const decide = vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "拒否" }));

        expect(decide).not.toHaveBeenCalled();
        const dialog = screen.getByRole("dialog", { name: "フォローリクエストを拒否しますか？" });
        await user.click(within(dialog).getByRole("button", { name: "拒否" }));
        expect(decide).toHaveBeenCalledWith("csrf", "alice", "bob", "rejected");
    });

    it("rejects and blocks a follow requester after confirmation", async () => {
        const item = { id: "follow-3", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        const decide = vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "拒否してブロック" }));

        expect(decide).not.toHaveBeenCalled();
        const dialog = screen.getByRole("dialog", { name: "フォローを拒否してブロックしますか？" });
        await user.click(within(dialog).getByRole("button", { name: "拒否してブロック" }));
        expect(decide).toHaveBeenCalledWith("csrf", "alice", "bob", "rejected_and_blocked");
        expect(screen.queryByText("@bob")).not.toBeInTheDocument();
    });

    it("shows follow request actions in a bottom drawer on mobile", async () => {
        vi.stubGlobal(
            "matchMedia",
            vi.fn((query: string) => ({
                matches: query === "(width < 64rem)",
                media: query,
                onchange: null,
                addEventListener: vi.fn(),
                removeEventListener: vi.fn(),
                addListener: vi.fn(),
                removeListener: vi.fn(),
                dispatchEvent: vi.fn(),
            })),
        );
        const item = { id: "follow-mobile", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "Bobへの対応" }));

        const drawer = screen.getByRole("dialog", { name: "Bobへの対応の選択" });
        expect(drawer).toHaveAttribute("data-drawer", "bottom");
        await user.click(within(drawer).getByRole("option", { name: /承認してフォローバック/ }));
        expect(await screen.findByRole("dialog", { name: "承認してフォローバックしますか？" })).toHaveAttribute("data-drawer", "bottom");
    });

    it("renders empty and error states without losing the page controls", async () => {
        vi.spyOn(api, "followRequests").mockResolvedValue([]);
        vi.spyOn(api, "notifications").mockRejectedValue(new Error("offline"));
        const { rerender } = render(<FollowRequestsPage actorID="alice" csrf="csrf" onOpenProfile={vi.fn()} refreshKey={0} />);
        expect(await screen.findByText("保留中のリクエストはありません。")).toBeInTheDocument();

        rerender(<NotificationsPage actorID="alice" csrf="csrf" onActorChange={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} refreshKey={0} />);
        expect(await screen.findByRole("alert")).toHaveTextContent("offline");
        expect(screen.getByRole("button", { name: "このActor" })).toBeInTheDocument();
    });
});
