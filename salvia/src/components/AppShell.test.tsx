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

describe("AppShell navigation and mobile account controls", () => {
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

        expect(screen.getByText("Rosemary")).toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "Aliceのプロフィールを開く" }));
        await user.click(screen.getByRole("button", { name: "モバイルで操作するActor" }));
        await user.click(screen.getByRole("option", { name: /@bob/ }));
        await user.click(screen.getByRole("button", { name: "設定を開く" }));

        expect(onActorChange).toHaveBeenCalledWith("bob");
        expect(onNavigate).toHaveBeenCalledWith("/profiles/alice");
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

    it("keeps the mobile navigation above the iOS home indicator", () => {
        render(
            <AppShell actors={actors} onActorChange={vi.fn()} onCompose={vi.fn()} onLogout={vi.fn()} onNavigate={vi.fn()} page="home" selectedActor={actors[0]} session={session}>
                content
            </AppShell>,
        );

        const navigation = screen.getByRole("navigation", { name: "モバイルナビゲーション" });
        expect(navigation.getAttribute("style")).toContain("height: calc(4.25rem + env(safe-area-inset-bottom))");
        expect(navigation).toHaveStyle({ boxSizing: "border-box" });
        expect(document.head.querySelector("style[data-salvia-css]")?.textContent).toContain("padding-bottom:env(safe-area-inset-bottom)");
    });

    it("replaces the discovery timeline with custom emoji management", async () => {
        const navigate = vi.fn();
        const user = userEvent.setup();
        render(
            <AppShell actors={actors} onActorChange={vi.fn()} onCompose={vi.fn()} onLogout={vi.fn()} onNavigate={navigate} page="emojis" selectedActor={actors[0]} session={session}>
                content
            </AppShell>,
        );

        expect(screen.queryByRole("button", { name: "みつける" })).not.toBeInTheDocument();
        const links = screen.getAllByRole("button", { name: "絵文字" });
        await user.click(links[0]);
        expect(navigate).toHaveBeenCalledWith("/emojis");
    });

    it("shows the selected Actor's unread notification count in desktop and mobile navigation", () => {
        render(
            <AppShell actors={actors} onActorChange={vi.fn()} onCompose={vi.fn()} onLogout={vi.fn()} onNavigate={vi.fn()} page="home" selectedActor={actors[0]} session={session} unreadNotificationCount={123}>
                content
            </AppShell>,
        );

        const notificationButtons = screen.getAllByRole("button", { name: "通知（未読123件）", hidden: true });
        expect(notificationButtons).toHaveLength(2);
        for (const button of notificationButtons) expect(button).toHaveTextContent("99+");
    });
});
