import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Actor, Note } from "../lib/schema";
import { NotePage } from "./NotePage";

const author = { id: "alice", name: "Alice", username: "alice", uri: "https://example.test/users/alice" } as Actor;

const makeNote = (id: string, text: string, replyID?: string) =>
    ({
        id,
        uri: `https://example.test/notes/${id}`,
        text,
        sensitive: false,
        visibility: "public",
        created_at: "2026-09-01T00:00:00Z",
        author,
        attachments: [],
        emojis: [],
        reactions: [],
        mention_uris: [],
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

        render(<NotePage actorID="alice" csrf="csrf" emojis={[]} noteID={root.id} onBack={vi.fn()} onCompose={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByText("最初のノート")).toBeInTheDocument();
        const ancestors = screen.getAllByRole("article", { name: "会話の前のノート" });
        expect(ancestors).toHaveLength(2);
        expect(ancestors[0]).toHaveTextContent("最初のノート");
        expect(ancestors[1]).toHaveTextContent("ひとつ前のノート");
        expect(screen.getByText("表示中のノート")).toBeInTheDocument();
        expect(await screen.findByText("返信への返信")).toBeInTheDocument();
        await waitFor(() => expect(thread).toHaveBeenCalledWith("alice", "child", expect.any(AbortSignal), 5));
    });
});
