import type { ReactNode } from "react";

import { IconBell, IconChevronDown, IconHome, IconLeaf2, IconLogout, IconPlus, IconSettings, IconUsersPlus, IconWorld } from "@tabler/icons-react";

import type { Page } from "../lib/api";
import type { Actor, Session } from "../lib/schema";
import { Avatar, Button } from "./ui";

const navigation: { icon: typeof IconHome; label: string; page: Page; path: string }[] = [
    { icon: IconHome, label: "ホーム", page: "home", path: "/" },
    { icon: IconWorld, label: "みつける", page: "public", path: "/public" },
    { icon: IconBell, label: "通知", page: "notifications", path: "/notifications" },
    { icon: IconUsersPlus, label: "リクエスト", page: "follow-requests", path: "/follow-requests" },
    { icon: IconSettings, label: "設定", page: "settings", path: "/settings" },
];

export function AppShell({
    actors,
    children,
    onActorChange,
    onCompose,
    onLogout,
    onNavigate,
    page,
    selectedActor,
    session,
}: {
    actors: Actor[];
    children: ReactNode;
    onActorChange: (id: string) => void;
    onCompose: () => void;
    onLogout: () => void;
    onNavigate: (path: string) => void;
    page: Page;
    selectedActor: Actor;
    session: Session;
}) {
    return (
        <div className="app-layout">
            <aside className="sidebar">
                <button className="wordmark" onClick={() => onNavigate("/")} type="button">
                    <span>
                        <IconLeaf2 />
                    </span>
                    Salvia
                </button>
                <nav aria-label="メインナビゲーション">
                    {navigation.map((item) => (
                        <button aria-current={page === item.page ? "page" : undefined} className={page === item.page ? "nav-item nav-item--active" : "nav-item"} key={item.page} onClick={() => onNavigate(item.path)} type="button">
                            <item.icon />
                            {item.label}
                        </button>
                    ))}
                </nav>
                <Button className="compose-button" onClick={onCompose}>
                    <IconPlus />
                    ノート
                </Button>
                <div className="account-switcher">
                    <Avatar actor={selectedActor} size="small" />
                    <label>
                        <span>{session.display_name || session.username}</span>
                        <select aria-label="操作するActor" onChange={(event) => onActorChange(event.target.value)} value={selectedActor.id}>
                            {actors.map((actor) => (
                                <option key={actor.id} value={actor.id}>
                                    @{actor.username}
                                </option>
                            ))}
                        </select>
                    </label>
                    <IconChevronDown />
                </div>
                <button className="logout-button" onClick={onLogout} type="button">
                    <IconLogout />
                    ログアウト
                </button>
            </aside>
            <main className="main-column">{children}</main>
            <nav aria-label="モバイルナビゲーション" className="mobile-nav">
                {navigation.slice(0, 4).map((item) => (
                    <button aria-current={page === item.page ? "page" : undefined} key={item.page} onClick={() => onNavigate(item.path)} type="button">
                        <item.icon />
                        <span>{item.label}</span>
                    </button>
                ))}
                <button aria-label="ノートを作成" className="mobile-compose" onClick={onCompose} type="button">
                    <IconPlus />
                </button>
            </nav>
        </div>
    );
}
