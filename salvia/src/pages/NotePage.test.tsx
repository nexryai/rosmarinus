import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "@/lib/api";
import type { Actor, Note } from "@/lib/schema";
import { NotePage } from "@/pages/NotePage";

const author = { id: "alice", name: "Alice", username: "alice", uri: "https://example.test/users/alice" } as Actor;

const makeNote = (id: string, text: string, replyID?: string) =>
    ({
        id,
        uri: `https://example.test/notes/${id}`,
        text,
        sensitive: false,
        visibility: "public",
        created_at: "2026-09-01T00:00:00Z",
        replies_count: 0,
        author,
        attachments: [],
        emojis: [],
        reactions: [],
        mention_uris: [],
        mentions: [],
        hashtags: [],
        ...(replyID ? { reply_id: replyID } : {}),
    }) as Note;

describe("NotePage conversation", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("shows ancestors oldest-first and expands nested replies", async () => {
        const grandparent = makeNote("grandparent", "最初のノート");
        const parent = makeNote("parent", "ひとつ前のノート", grandparent.id);
        const root = makeNote("root", "表示中のノート", parent.id);
        const child = makeNote("child", "直接の返信", root.id);
        const grandchild = makeNote("grandchild", "返信への返信", child.id);
        const notes = new Map([grandparent, parent, root].map((item) => [item.id, item]));
        vi.spyOn(api, "note").mockImplementation(async (_actorID, noteID) => {
            const result = notes.get(noteID);
            if (!result) throw new Error("not found");
            return result;
        });
        const thread = vi.spyOn(api, "thread").mockImplementation(async (_actorID, noteID) => {
            if (noteID === root.id) return [child];
            if (noteID === child.id) return [grandchild];
            return [];
        });

        const onOpenNote = vi.fn();
        render(<NotePage actorID="alice" csrf="csrf" emojis={[]} noteID={root.id} onBack={vi.fn()} onCompose={vi.fn()} onOpenNote={onOpenNote} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByText("最初のノート")).toBeInTheDocument();
        const ancestors = screen.getAllByRole("article", { name: "会話の前のノート" });
        expect(ancestors).toHaveLength(2);
        expect(ancestors[0]).toHaveTextContent("最初のノート");
        expect(ancestors[1]).toHaveTextContent("ひとつ前のノート");
        expect(screen.getByText("表示中のノート")).toBeInTheDocument();
        const reactionSection = screen.getByRole("region", { name: "リアクション" });
        expect(screen.getByText("表示中のノート").compareDocumentPosition(reactionSection) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(reactionSection.compareDocumentPosition(screen.getByRole("heading", { name: "返信" })) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
        expect(await screen.findByText("返信への返信")).toBeInTheDocument();
        await waitFor(() => expect(thread).toHaveBeenCalledWith("alice", "child", expect.any(AbortSignal), 5));
        for (const reply of screen.getAllByRole("article", { name: "返信ノート" })) {
            expect(reply.querySelector("i")).toBeNull();
            expect(reply.children[1]).toHaveStyle({ background: "var(--panel-muted)", "border-radius": "0px 10px 10px 10px", "margin-left": "6px" });
        }
        await userEvent.setup().click(screen.getByText("返信への返信"));
        expect(onOpenNote).toHaveBeenCalledWith("grandchild");
    });

    it("shows the original Note's reaction identities when viewing a renote", async () => {
        const original = makeNote("original", "リノート元の本文");
        original.reactions = [{ reaction: "👍", count: 1, reacted: false }];
        const root = { ...makeNote("renote", ""), renote_id: original.id, renote: original };
        vi.spyOn(api, "note").mockResolvedValue(root);
        vi.spyOn(api, "thread").mockResolvedValue([]);
        const reactions = vi.spyOn(api, "noteReactionsPage").mockResolvedValue({ data: [{ id: "bob", username: "bob", name: "Bob", avatar_url: "", uri: "https://remote.test/users/bob", emojis: [] }], next: "" });

        render(<NotePage actorID="alice" csrf="csrf" emojis={[]} noteID={root.id} onBack={vi.fn()} onCompose={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByText("Bob")).toBeInTheDocument();
        expect(reactions).toHaveBeenCalledWith("alice", "original", "👍", { signal: expect.any(AbortSignal) });
        expect(screen.getByRole("region", { name: "リアクション" })).toBeInTheDocument();
    });
});
