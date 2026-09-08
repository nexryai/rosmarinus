import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Note } from "../lib/schema";
import { TimelinePage } from "./TimelinePage";

describe("TimelinePage remote notes", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
        vi.unstubAllGlobals();
    });

    it.each([[{ name: "Website", value: "https://remote.test" }], [{ Name: "Website", Value: "https://remote.test" }]])("renders a remote author with profile fields %j", async (field) => {
        const fetchMock = vi.fn().mockResolvedValue(
            new Response(
                JSON.stringify({
                    version: 1,
                    data: [
                        {
                            id: "remote-note",
                            uri: "https://remote.test/notes/1",
                            text: "Hello from a remote user",
                            sensitive: false,
                            visibility: "public",
                            created_at: "2026-09-05T00:00:00Z",
                            author: {
                                id: "remote-actor",
                                username: "remote",
                                uri: "https://remote.test/users/1",
                                profile_fields: [field],
                            },
                        },
                    ],
                    next: "",
                }),
                { headers: { "Content-Type": "application/json" } },
            ),
        );
        vi.stubGlobal("fetch", fetchMock);

        render(<TimelinePage actorID="local-actor" csrf="csrf" emojis={[]} kind="home" liveRefreshKey={0} onCompose={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByText("Hello from a remote user")).toBeInTheDocument();
        expect(screen.getByText("@remote")).toBeInTheDocument();
        expect(fetchMock).toHaveBeenCalledWith("/api/v1/timelines/home?actor_id=local-actor&limit=30", expect.objectContaining({ credentials: "same-origin" }));
    });

    it("animates only notes newly added by a live refresh", async () => {
        const original = {
            id: "note-old",
            uri: "https://remote.test/notes/old",
            text: "すでにあるノート",
            sensitive: false,
            visibility: "public",
            created_at: "2026-09-08T00:00:00Z",
            attachments: [],
            emojis: [],
            reactions: [],
        } as unknown as Note;
        const arrived = { ...original, id: "note-new", uri: "https://remote.test/notes/new", text: "SSEで届いたノート" };
        const manuallyFound = { ...original, id: "note-manual", uri: "https://remote.test/notes/manual", text: "手動更新で見つけたノート" };
        vi.spyOn(api, "timeline")
            .mockResolvedValueOnce({ data: [original], next: "" })
            .mockResolvedValueOnce({ data: [arrived, original], next: "" })
            .mockResolvedValueOnce({ data: [manuallyFound, arrived, original], next: "" });
        const props = { actorID: "local-actor", csrf: "csrf", emojis: [], kind: "home" as const, onCompose: vi.fn(), onOpenNote: vi.fn(), onOpenProfile: vi.fn() };
        const { rerender } = render(<TimelinePage {...props} liveRefreshKey={0} refreshKey={0} />);

        expect(await screen.findByText("すでにあるノート")).toBeInTheDocument();
        expect(document.querySelector("[data-live-entry]")).toBeNull();

        rerender(<TimelinePage {...props} liveRefreshKey={1} refreshKey={1} />);

        const newText = await screen.findByText("SSEで届いたノート");
        const animatedSlot = newText.closest('[data-live-entry="true"]');
        expect(animatedSlot).not.toBeNull();
        expect(screen.getByText("すでにあるノート").closest("[data-live-entry]")).toBeNull();

        rerender(<TimelinePage {...props} liveRefreshKey={1} refreshKey={2} />);
        expect(await screen.findByText("手動更新で見つけたノート")).toBeInTheDocument();
        expect(document.querySelector("[data-live-entry]")).toBeNull();
    });
});
