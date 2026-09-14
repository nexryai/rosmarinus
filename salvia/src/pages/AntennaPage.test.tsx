import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import { AntennaPage } from "./AntennaPage";

describe("AntennaPage", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("creates an Actor-owned antenna from the editor", async () => {
        vi.spyOn(api, "antennas").mockResolvedValue([]);
        const create = vi.spyOn(api, "createAntenna").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<AntennaPage actorID="actor-1" csrf="csrf" emojis={[]} onCompose={vi.fn()} onOpenNote={vi.fn()} onOpenProfile={vi.fn()} />);

        await user.click(await screen.findByRole("button", { name: "作成" }));
        await user.type(screen.getByRole("textbox", { name: "名前" }), "ActivityPub");
        await user.type(screen.getByRole("textbox", { name: /^含めるキーワード/ }), "Go ActivityPub");
        await user.click(screen.getByRole("button", { name: "保存" }));

        expect(create).toHaveBeenCalledWith("csrf", "actor-1", expect.objectContaining({ name: "ActivityPub", keywords: [["Go", "ActivityPub"]], source: "all" }));
    });
});
