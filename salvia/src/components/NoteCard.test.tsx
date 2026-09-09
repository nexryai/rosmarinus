import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Actor, Note } from "../lib/schema";
import { NoteCard } from "./NoteCard";

const author = { id: "bob", username: "bob", name: "Bob" } as Actor;
const note = { id: "note-1", uri: "https://example.test/notes/1", text: "hello", visibility: "public", created_at: new Date().toISOString(), author, attachments: [], emojis: [], reactions: [] } as unknown as Note;

describe("NoteCard social actions", () => {
    afterEach(() => {
        cleanup();
        delete document.documentElement.dataset.reduceMotion;
    });

    it("exposes reply, quote, renote, and reaction actions", async () => {
        const user = userEvent.setup();
        const onReply = vi.fn();
        const onQuote = vi.fn();
        const onRenote = vi.fn().mockResolvedValue(undefined);
        const onReact = vi.fn().mockResolvedValue(undefined);
        render(<NoteCard note={note} ownActorID="alice" onDelete={vi.fn()} onOpenProfile={vi.fn()} onQuote={onQuote} onReact={onReact} onRenote={onRenote} onReply={onReply} onVote={vi.fn()} />);

        await user.click(screen.getByRole("button", { name: "返信" }));
        await user.click(screen.getByRole("button", { name: "引用" }));
        await user.click(screen.getByRole("button", { name: "リノート" }));
        await user.click(screen.getByRole("button", { name: "リアクションを追加" }));
        await user.click(screen.getByRole("button", { name: "👍" }));

        expect(onReply).toHaveBeenCalledWith(note);
        expect(onQuote).toHaveBeenCalledWith(note);
        expect(onRenote).toHaveBeenCalledWith(note);
        expect(onReact).toHaveBeenCalledWith(note.id, "👍", false);
    });

    it("renders custom emoji without injecting markup", () => {
        const emojiNote = { ...note, text: "Hi :salvia: <script>", emojis: [{ name: "salvia", url: "/media/salvia", media_type: "image/webp" }] };
        render(<NoteCard note={emojiNote} ownActorID="alice" onDelete={vi.fn()} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        const emoji = screen.getByAltText(":salvia:");
        expect(emoji).toHaveAttribute("src", "/media/salvia");
        expect(emoji.parentElement).toHaveTextContent("<script>");
        expect(document.querySelector("script")).toBeNull();
    });

    it("renders MFM and remote custom emoji reactions", () => {
        const emojiNote = {
            ...note,
            text: "**強調された本文**",
            reactions: [{ reaction: ":party@remote.test:", count: 3, reacted: false, emoji: { name: "party", url: "https://remote.test/party.webp", media_type: "image/webp" } }],
        } as Note;
        render(<NoteCard note={emojiNote} ownActorID="alice" onDelete={vi.fn()} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        expect(screen.getByText("強調された本文").tagName).toBe("STRONG");
        expect(screen.getByAltText(":party@remote.test:")).toHaveAttribute("src", "https://remote.test/party.webp");
        expect(screen.getByText("3")).toBeInTheDocument();
    });

    it("opens image attachments in the custom viewer", () => {
        document.documentElement.dataset.reduceMotion = "true";
        const imageNote = {
            ...note,
            attachments: [
                {
                    media_type: "image/jpeg",
                    name: "庭のサルビア",
                    sensitive: false,
                    url: "https://media.example.test/salvia.jpg",
                },
            ],
        } as Note;
        render(<NoteCard note={imageNote} ownActorID="alice" onDelete={vi.fn()} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        fireEvent.click(screen.getByRole("button", { name: "画像を表示: 庭のサルビア" }));
        expect(screen.getByRole("dialog", { name: "画像ビューアー" })).toBeInTheDocument();
        expect(screen.getByRole("link", { name: "元の画像を開く" })).toHaveAttribute("href", imageNote.attachments[0].url);

        fireEvent.keyDown(document, { key: "Escape" });
        expect(screen.queryByRole("dialog", { name: "画像ビューアー" })).not.toBeInTheDocument();
    });

    it("renders a Twitter-style quoted note with its author identity", () => {
        const onOpenNote = vi.fn();
        const onOpenProfile = vi.fn();
        const quoted = {
            ...note,
            quote: {
                id: "quote-1",
                uri: "https://remote.test/notes/quote-1",
                text: "引用された本文",
                sensitive: false,
                visibility: "public",
                created_at: "2026-09-01T00:00:00Z",
                author: {
                    ...author,
                    avatar_url: "https://remote.test/avatar.jpg",
                    uri: "https://remote.test/users/bob",
                },
            },
        } as Note;
        render(<NoteCard note={quoted} ownActorID="alice" onDelete={vi.fn()} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        expect(screen.getByAltText("Bobのアバター")).toHaveAttribute("src", "https://remote.test/avatar.jpg");
        expect(screen.getByText("@bob@remote.test")).toBeInTheDocument();
        expect(screen.getByText("引用された本文")).toBeInTheDocument();
        fireEvent.click(screen.getByAltText("Bobのアバター").closest("button") as HTMLButtonElement);
        fireEvent.click(screen.getByRole("button", { name: "引用ノートを開く: Bob" }));

        expect(onOpenProfile).toHaveBeenCalledWith("bob");
        expect(onOpenNote).toHaveBeenCalledWith("quote-1");
    });

    it("renders a renote as Misskey-style attribution followed by the original note", async () => {
        const user = userEvent.setup();
        const onOpenNote = vi.fn();
        const onOpenProfile = vi.fn();
        const onReply = vi.fn();
        const originalAuthor = {
            ...author,
            id: "remote-bob",
            avatar_url: "https://remote.test/avatar.jpg",
            uri: "https://remote.test/users/bob",
        };
        const renote = {
            ...note,
            id: "renote-1",
            author: { ...author, id: "alice", name: "Alice", username: "alice" },
            renote_id: "original-1",
            renote: {
                id: "original-1",
                uri: "https://remote.test/notes/original-1",
                text: "元ノート :salvia:",
                sensitive: false,
                visibility: "public",
                created_at: "2026-09-01T00:00:00Z",
                author: originalAuthor,
                emojis: [{ name: "salvia", url: "https://remote.test/salvia.webp", media_type: "image/webp" }],
                attachments: [{ media_type: "image/jpeg", name: "元ノートの画像", sensitive: false, url: "https://remote.test/image.jpg" }],
                quote: {
                    id: "quoted-note",
                    uri: "https://remote.test/notes/quoted",
                    text: "引用元の本文",
                    sensitive: false,
                    visibility: "public",
                    created_at: "2026-08-31T00:00:00Z",
                    author: { ...author, id: "carol", name: "Carol", username: "carol", uri: "https://quoted.test/users/carol" },
                    emojis: [],
                    attachments: [],
                },
            },
        } as Note;
        render(<NoteCard note={renote} ownActorID="carol" onDelete={vi.fn()} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={onReply} onVote={vi.fn()} />);

        expect(screen.getByText("Aliceさんがリノート")).toBeInTheDocument();
        expect(screen.getByText("@bob@remote.test")).toBeInTheDocument();
        expect(screen.getByAltText(":salvia:")).toHaveAttribute("src", "https://remote.test/salvia.webp");
        expect(screen.getByRole("button", { name: "画像を表示: 元ノートの画像" })).toBeInTheDocument();
        expect(screen.getByText("引用元の本文")).toBeInTheDocument();

        await user.click(screen.getByRole("button", { name: /ノートの詳細を開く/ }));
        await user.click(screen.getByRole("button", { name: "返信" }));
        await user.click(screen.getByRole("button", { name: "Aliceさんがリノート" }));
        await user.click(screen.getByRole("button", { name: "引用ノートを開く: Carol" }));

        expect(onOpenNote).toHaveBeenCalledWith("original-1");
        expect(onOpenNote).toHaveBeenCalledWith("quoted-note");
        expect(onReply).toHaveBeenCalledWith(expect.objectContaining({ id: "original-1", author: originalAuthor }));
        expect(onOpenProfile).toHaveBeenCalledWith("alice");
    });

    it("shows a deleted-note placeholder when a renote target is unavailable", () => {
        const deletedRenote = { ...note, renote_id: "deleted-note" } as Note;
        render(<NoteCard note={deletedRenote} ownActorID="alice" onDelete={vi.fn()} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        expect(screen.getByText("削除されたノート")).toBeInTheDocument();
        expect(screen.queryByRole("button", { name: "返信" })).not.toBeInTheDocument();
    });

    it("uses the timestamp as the detail link and an icon for visibility", async () => {
        const user = userEvent.setup();
        const onOpenNote = vi.fn();
        const followersNote = { ...note, created_at: "2026-09-01T00:00:00Z", visibility: "followers" } as Note;
        const { container } = render(<NoteCard note={followersNote} ownActorID="alice" onDelete={vi.fn()} onOpenNote={onOpenNote} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        const timestamp = screen.getByRole("button", { name: /ノートの詳細を開く/ });
        expect(timestamp).toContainElement(container.querySelector('time[datetime="2026-09-01T00:00:00Z"]'));
        expect(screen.queryByText("詳細")).not.toBeInTheDocument();
        expect(screen.getByRole("img", { name: "公開範囲: フォロワー" }).querySelector("svg")).toHaveClass("tabler-icon-lock");

        await user.click(timestamp);
        expect(onOpenNote).toHaveBeenCalledWith("note-1");
    });

    it("votes in a poll and confirms deletion of an owned note", async () => {
        const onVote = vi.fn().mockResolvedValue(undefined);
        const onDelete = vi.fn().mockResolvedValue(undefined);
        const pollNote = { ...note, author: { ...author, id: "alice" }, poll: { choices: [{ index: 0, text: "A", votes: 0, voted: false }], multiple: false, expires_at: null, expired: false } } as Note;
        const user = userEvent.setup();
        render(<NoteCard note={pollNote} ownActorID="alice" onDelete={onDelete} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={onVote} />);

        await user.click(screen.getByRole("button", { name: /A/ }));
        await user.click(screen.getByRole("button", { name: "削除" }));

        expect(onVote).toHaveBeenCalledWith("note-1", 0);
        expect(onDelete).not.toHaveBeenCalled();
        expect(screen.getByRole("dialog", { name: "ノートを削除しますか？" })).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "削除する" }));
        expect(onDelete).toHaveBeenCalledWith("note-1");
    });

    it("removes an existing reaction", async () => {
        const onReact = vi.fn().mockResolvedValue(undefined);
        const reacted = { ...note, reactions: [{ reaction: "❤️", count: 2, reacted: true }] } as Note;
        const user = userEvent.setup();
        render(<NoteCard note={reacted} ownActorID="alice" onDelete={vi.fn()} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={onReact} onRenote={vi.fn()} onReply={vi.fn()} onVote={vi.fn()} />);

        const reaction = screen.getByRole("button", { name: "❤️2" });
        expect(reaction).toHaveStyle({ background: "var(--accent-soft)", color: "var(--accent-ink)" });
        expect(reaction.style.borderColor).toBe("var(--accent)");
        await user.click(reaction);

        expect(onReact).toHaveBeenCalledWith("note-1", "❤️", true);
    });
});
