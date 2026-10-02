import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { NoteReactions } from "@/components/NoteReactions";
import { api } from "@/lib/api";
import type { Note, ReactionActor } from "@/lib/schema";

const alice: ReactionActor = { id: "alice", name: "Alice", username: "alice", avatar_url: "", uri: "https://example.test/users/alice", emojis: [] };
const bob: ReactionActor = { ...alice, id: "bob", name: "Bob", username: "bob" };
const note = (reactions: Note["reactions"]): Note => ({ id: "note-1", reactions }) as Note;
const thumbsUp = { reaction: "👍", count: 2, reacted: false };

describe("Note reaction details", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("groups people by Unicode and custom emoji reactions and opens profiles", async () => {
        const reactions = vi.spyOn(api, "noteReactionsPage").mockImplementation(async (_actorID, _noteID, reaction) => ({ data: reaction === "👍" ? [alice] : [bob], next: "" }));
        const onOpenProfile = vi.fn();
        const custom = { reaction: ":party@remote.test:", count: 1, reacted: false, emoji: { name: "party", url: "https://remote.test/party.webp" } };
        render(<NoteReactions actorID="viewer" note={note([thumbsUp, custom])} onOpenProfile={onOpenProfile} />);

        const unicodeGroup = screen.getByRole("region", { name: "👍のリアクション" });
        const customGroup = screen.getByRole("region", { name: ":party@remote.test:のリアクション" });
        expect(await within(unicodeGroup).findByText("Alice")).toBeInTheDocument();
        expect(within(unicodeGroup).getByText("2件")).toBeInTheDocument();
        expect(await within(customGroup).findByText("Bob")).toBeInTheDocument();
        expect(within(customGroup).getByAltText(":party@remote.test:")).toHaveAttribute("src", "https://remote.test/party.webp");
        expect(reactions).toHaveBeenCalledWith("viewer", "note-1", "👍", { signal: expect.any(AbortSignal) });
        await userEvent.setup().click(within(customGroup).getByRole("button", { name: "Bobのプロフィールを開く" }));
        expect(onOpenProfile).toHaveBeenCalledWith("bob");
    });

    it("loads more people using the backend cursor and removes duplicate identities", async () => {
        const reactions = vi
            .spyOn(api, "noteReactionsPage")
            .mockResolvedValueOnce({ data: [alice], next: "next-page" })
            .mockResolvedValueOnce({ data: [alice, bob], next: "" });
        render(<NoteReactions actorID="viewer" note={note([thumbsUp])} onOpenProfile={vi.fn()} />);

        await userEvent.setup().click(await screen.findByRole("button", { name: "もっと見る" }));

        expect(await screen.findByText("Bob")).toBeInTheDocument();
        expect(screen.getAllByText("Alice")).toHaveLength(1);
        expect(reactions).toHaveBeenLastCalledWith("viewer", "note-1", "👍", { after: "next-page", signal: expect.any(AbortSignal) });
        expect(screen.queryByRole("button", { name: "もっと見る" })).not.toBeInTheDocument();
    });

    it("retries a failed page without losing people already displayed", async () => {
        vi.spyOn(api, "noteReactionsPage")
            .mockResolvedValueOnce({ data: [alice], next: "next-page" })
            .mockRejectedValueOnce(new Error("offline"))
            .mockResolvedValueOnce({ data: [bob], next: "" });
        render(<NoteReactions actorID="viewer" note={note([thumbsUp])} onOpenProfile={vi.fn()} />);
        const user = userEvent.setup();

        await user.click(await screen.findByRole("button", { name: "もっと見る" }));
        expect(await screen.findByRole("alert")).toHaveTextContent("リアクションしたユーザーを読み込めませんでした");
        expect(screen.getByText("Alice")).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "再試行" }));
        expect(await screen.findByText("Bob")).toBeInTheDocument();
        expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    });

    it("refreshes identities when the Note updates and ignores aborted results", async () => {
        let finishOldRequest!: (page: { data: ReactionActor[]; next: string }) => void;
        const reactions = vi
            .spyOn(api, "noteReactionsPage")
            .mockReturnValueOnce(
                new Promise((resolve) => {
                    finishOldRequest = resolve;
                }),
            )
            .mockResolvedValueOnce({ data: [bob], next: "" });
        const props = { actorID: "viewer", onOpenProfile: vi.fn() };
        const { rerender } = render(<NoteReactions {...props} note={note([thumbsUp])} />);
        const signal = reactions.mock.calls[0][3]?.signal;

        rerender(<NoteReactions {...props} note={note([{ ...thumbsUp, count: 1 }])} />);

        expect(signal?.aborted).toBe(true);
        expect(await screen.findByText("Bob")).toBeInTheDocument();
        await act(async () => finishOldRequest({ data: [alice], next: "old-page" }));
        expect(screen.queryByText("Alice")).not.toBeInTheDocument();
        expect(screen.getByText("1件")).toBeInTheDocument();
        await waitFor(() => expect(reactions).toHaveBeenCalledTimes(2));
    });

    it("shows empty states for Notes without reactions and filtered reactor lists", async () => {
        const reactions = vi.spyOn(api, "noteReactionsPage").mockResolvedValue({ data: [], next: "" });
        const props = { actorID: "viewer", onOpenProfile: vi.fn() };
        const { rerender } = render(<NoteReactions {...props} note={note([])} />);
        expect(screen.getByText("まだリアクションはありません。")).toBeInTheDocument();
        expect(reactions).not.toHaveBeenCalled();

        rerender(<NoteReactions {...props} note={note([thumbsUp])} />);
        expect(await screen.findByText("表示できるユーザーはいません。")).toBeInTheDocument();
    });
});
