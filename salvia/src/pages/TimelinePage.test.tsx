import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { TimelinePage } from "./TimelinePage";

describe("TimelinePage remote notes", () => {
    afterEach(() => {
        cleanup();
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

        render(<TimelinePage actorID="local-actor" csrf="csrf" emojis={[]} kind="home" onCompose={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} refreshKey={0} />);

        expect(await screen.findByText("Hello from a remote user")).toBeInTheDocument();
        expect(screen.getByText("@remote")).toBeInTheDocument();
        expect(fetchMock).toHaveBeenCalledWith("/api/v1/timelines/home?actor_id=local-actor&limit=30", expect.objectContaining({ credentials: "same-origin" }));
    });
});
