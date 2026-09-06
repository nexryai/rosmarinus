import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { api } from "../lib/api";
import type { Profile } from "../lib/schema";
import { UserSearchPage } from "./UserSearchPage";

describe("UserSearchPage", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("resolves a remote handle and opens its profile", async () => {
        const profile = { actor: { id: "remote-alice" } } as Profile;
        const resolveProfile = vi.spyOn(api, "resolveProfile").mockResolvedValue(profile);
        const onOpenProfile = vi.fn();
        const user = userEvent.setup();
        render(<UserSearchPage actorID="local-bob" csrf="csrf" onOpenProfile={onOpenProfile} />);

        await user.type(screen.getByRole("textbox", { name: "ハンドルまたはプロフィールURL" }), " @alice@example.com ");
        await user.click(screen.getByRole("button", { name: "プロフィールを表示" }));

        expect(resolveProfile).toHaveBeenCalledWith("csrf", "local-bob", "@alice@example.com");
        expect(onOpenProfile).toHaveBeenCalledWith("remote-alice");
    });
});
