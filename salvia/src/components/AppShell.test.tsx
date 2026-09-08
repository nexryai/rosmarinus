import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Actor, Session } from "../lib/schema";
import { AppShell } from "./AppShell";
import { PageHeader } from "./ui";

const actors = [
    { id: "alice", username: "alice", name: "Alice" },
    { id: "bob", username: "bob", name: "Bob" },
] as Actor[];
const session = { account_id: "account", csrf_token: "csrf", username: "owner", display_name: "Owner" } as Session;

describe("AppShell mobile account controls", () => {
    afterEach(cleanup);

    it("keeps settings and Actor switching reachable from the mobile header", async () => {
        const user = userEvent.setup();
        const onActorChange = vi.fn();
        const onNavigate = vi.fn();
        render(
            <AppShell actors={actors} onActorChange={onActorChange} onCompose={vi.fn()} onLogout={vi.fn()} onNavigate={onNavigate} page="home" selectedActor={actors[0]} session={session}>
                <p>content</p>
            </AppShell>,
        );

        await user.click(screen.getByRole("button", { name: "モバイルで操作するActor" }));
        await user.click(screen.getByRole("option", { name: /@bob/ }));
        await user.click(screen.getByRole("button", { name: "設定を開く" }));

        expect(onActorChange).toHaveBeenCalledWith("bob");
        expect(onNavigate).toHaveBeenCalledWith("/settings");
    });

    it("hides page headers beneath the mobile shell header", () => {
        render(
            <AppShell actors={actors} onActorChange={vi.fn()} onCompose={vi.fn()} onLogout={vi.fn()} onNavigate={vi.fn()} page="home" selectedActor={actors[0]} session={session}>
                <PageHeader eyebrow="タイムライン" title="ホーム" />
            </AppShell>,
        );

        expect(document.querySelector("main > header")).not.toBeVisible();
        expect(screen.getByRole("banner", { name: "モバイルアカウント操作" })).toBeVisible();
    });
});
