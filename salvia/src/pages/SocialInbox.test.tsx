import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Actor, Connection, Notification } from "../lib/schema";
import { FollowRequestsPage } from "./FollowRequestsPage";
import { NotificationsPage } from "./NotificationsPage";

const remote = { id: "bob", username: "bob", name: "Bob" } as Actor;

describe("social inbox mutations", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("approves a mandatory follow request and removes it from the queue", async () => {
        const item = { id: "follow-1", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        const decide = vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "承認" }));

        expect(decide).toHaveBeenCalledWith("csrf", "alice", "bob", "accepted");
        expect(screen.queryByText("@bob")).not.toBeInTheDocument();
    });

    it("marks an Actor notification read", async () => {
        const item = { id: "notification-1", actor_id: "alice", kind: "follow", created_at: "2026-01-01T00:00:00Z", is_read: false, read_at: null, source: remote } as Notification;
        vi.spyOn(api, "notifications").mockResolvedValue([item]);
        const markRead = vi.spyOn(api, "markNotificationRead").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<NotificationsPage actorID="alice" csrf="csrf" onActorChange={vi.fn()} onOpenNote={vi.fn()} refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "既読" }));

        expect(markRead).toHaveBeenCalledWith("csrf", "alice", "notification-1");
        expect(screen.queryByRole("button", { name: "既読" })).not.toBeInTheDocument();
    });

    it("rejects a mandatory follow request", async () => {
        const item = { id: "follow-2", status: "pending", created_at: "2026-01-01T00:00:00Z", accepted_at: null, actor: remote } as Connection;
        vi.spyOn(api, "followRequests").mockResolvedValue([item]);
        const decide = vi.spyOn(api, "decideFollowRequest").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<FollowRequestsPage actorID="alice" csrf="csrf" refreshKey={0} />);

        await user.click(await screen.findByRole("button", { name: "拒否" }));

        expect(decide).toHaveBeenCalledWith("csrf", "alice", "bob", "rejected");
    });

    it("renders empty and error states without losing the page controls", async () => {
        vi.spyOn(api, "followRequests").mockResolvedValue([]);
        vi.spyOn(api, "notifications").mockRejectedValue(new Error("offline"));
        const { rerender } = render(<FollowRequestsPage actorID="alice" csrf="csrf" refreshKey={0} />);
        expect(await screen.findByText("保留中のリクエストはありません。")).toBeInTheDocument();

        rerender(<NotificationsPage actorID="alice" csrf="csrf" onActorChange={vi.fn()} onOpenNote={vi.fn()} refreshKey={0} />);
        expect(await screen.findByRole("alert")).toHaveTextContent("offline");
        expect(screen.getByRole("button", { name: "このActor" })).toBeInTheDocument();
    });
});
