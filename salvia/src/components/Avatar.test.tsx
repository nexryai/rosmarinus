import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Actor } from "../lib/schema";
import { Avatar } from "./ui";

const actor = { id: "remote-bob", username: "bob", name: "Bob", avatar_url: "https://remote.example/avatar.png" } as Actor;

describe("Avatar", () => {
    afterEach(cleanup);

    it("opens the represented Actor profile when interactive", async () => {
        const user = userEvent.setup();
        const onOpenProfile = vi.fn();
        render(<Avatar actor={actor} onOpenProfile={onOpenProfile} />);

        await user.click(screen.getByRole("button", { name: "Bobのプロフィールを開く" }));

        expect(onOpenProfile).toHaveBeenCalledWith("remote-bob");
        expect(screen.getByAltText("Bobのアバター")).toHaveAttribute("referrerpolicy", "no-referrer");
    });

    it("keeps fallback avatars available when no image URL exists", () => {
        render(<Avatar actor={{ ...actor, avatar_url: "" }} />);

        expect(screen.getByRole("img", { name: "Bobのアバター" })).toHaveTextContent("B");
    });
});
