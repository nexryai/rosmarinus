import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Actor, Note } from "../lib/schema";
import { NoteCard } from "./NoteCard";

const author = { id: "bob", username: "bob", name: "Bob" } as Actor;
const note = { id: "note-1", uri: "https://example.test/notes/1", text: "hello", visibility: "public", created_at: new Date().toISOString(), author, attachments: [], emojis: [], reactions: [] } as unknown as Note;

describe("NoteCard social actions", () => {
    afterEach(cleanup);

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

        expect(screen.getByAltText(":salvia:")).toHaveAttribute("src", "/media/salvia");
        expect(screen.getByText((_, element) => element?.tagName === "P" && element.textContent?.includes("<script>") === true)).toBeInTheDocument();
        expect(document.querySelector("script")).toBeNull();
    });

    it("votes in a poll and confirms deletion of an owned note", async () => {
        vi.spyOn(window, "confirm").mockReturnValue(true);
        const onVote = vi.fn().mockResolvedValue(undefined);
        const onDelete = vi.fn().mockResolvedValue(undefined);
        const pollNote = { ...note, author: { ...author, id: "alice" }, poll: { choices: [{ index: 0, text: "A", votes: 0, voted: false }], multiple: false, expires_at: null, expired: false } } as Note;
        const user = userEvent.setup();
        render(<NoteCard note={pollNote} ownActorID="alice" onDelete={onDelete} onOpenProfile={vi.fn()} onQuote={vi.fn()} onReact={vi.fn()} onRenote={vi.fn()} onReply={vi.fn()} onVote={onVote} />);

        await user.click(screen.getByRole("button", { name: /A/ }));
        await user.click(screen.getByRole("button", { name: "削除" }));

        expect(onVote).toHaveBeenCalledWith("note-1", 0);
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
