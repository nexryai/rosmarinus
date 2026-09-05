import type { CSSProperties, ReactNode } from "react";

import { IconBell, IconChevronDown, IconHome, IconLeaf2, IconLogout, IconPlus, IconSettings, IconUsersPlus, IconWorld } from "@tabler/icons-react";

import type { Page } from "../lib/api";
import { css } from "../lib/css";
import type { Actor, Session } from "../lib/schema";
import { Avatar, Button } from "./ui";

const navigation: { icon: typeof IconHome; label: string; page: Page; path: string }[] = [
    { icon: IconHome, label: "ホーム", page: "home", path: "/" },
    { icon: IconWorld, label: "みつける", page: "public", path: "/public" },
    { icon: IconBell, label: "通知", page: "notifications", path: "/notifications" },
    { icon: IconUsersPlus, label: "リクエスト", page: "follow-requests", path: "/follow-requests" },
    { icon: IconSettings, label: "設定", page: "settings", path: "/settings" },
];

const styles = {
    layout: { width: "100%", minHeight: "100dvh", maxWidth: "72rem", marginInline: "auto", display: "flex", background: "var(--panel)", boxShadow: "var(--shadow)" },
    sidebar: { position: "sticky", top: 0, width: "16rem", height: "100dvh", padding: "1.5rem 1.25rem", flexDirection: "column", flexShrink: 0, borderRight: "1px solid var(--border)" },
    wordmark: { marginBottom: "2rem", paddingInline: "0.75rem", display: "flex", alignItems: "center", gap: "0.75rem", borderRadius: "0.75rem", fontSize: "1.25rem", lineHeight: 1.4, fontWeight: 900, letterSpacing: "-0.025em" },
    mark: { width: "2.75rem", height: "2.75rem", display: "grid", placeItems: "center", borderRadius: "1rem", color: "var(--accent-ink)", background: "linear-gradient(135deg, #f8d56a, var(--accent))", boxShadow: "0 8px 22px #e9a91d3d" },
    navItem: { width: "100%", padding: "0.75rem 1rem", display: "flex", alignItems: "center", gap: "1rem", borderRadius: "0.75rem", textAlign: "left", fontWeight: 600, transition: "color 150ms, background-color 150ms" },
    navItemActive: { color: "var(--accent-ink)", background: "var(--accent-soft)" },
    compose: { width: "100%", marginTop: "1.5rem" },
    accountSwitcher: { marginTop: "auto", padding: "0.75rem", display: "flex", alignItems: "center", gap: "0.75rem", border: "1px solid var(--border)", borderRadius: "1rem", background: "var(--panel-muted)" },
    accountLabel: { minWidth: 0, flex: 1 },
    accountName: { display: "block", overflow: "hidden", fontSize: "0.875rem", fontWeight: 700, textOverflow: "ellipsis", whiteSpace: "nowrap" },
    actorSelect: { width: "100%", display: "block", appearance: "none", border: 0, outline: 0, color: "var(--muted)", background: "transparent", fontSize: "0.75rem" },
    logout: { marginTop: "0.5rem", padding: "0.5rem 0.75rem", display: "flex", alignItems: "center", gap: "0.5rem", borderRadius: "0.75rem", fontSize: "0.875rem", transition: "color 150ms, background-color 150ms" },
    main: { minWidth: 0, flex: 1, background: "var(--panel)" },
    mobileNav: { position: "fixed", zIndex: 30, insetInline: 0, bottom: 0, height: "4.25rem", paddingInline: "0.5rem", alignItems: "center", justifyContent: "space-around", borderTop: "1px solid var(--border)", backdropFilter: "blur(24px)" },
    mobileItem: { minWidth: "3.5rem", display: "grid", placeItems: "center", gap: "0.125rem", color: "var(--muted)", fontSize: "10px" },
    mobileCompose: { width: "2.75rem", height: "2.75rem", minWidth: "2.75rem", display: "grid", placeItems: "center", borderRadius: "9999px", color: "var(--accent-ink)", background: "var(--accent)" },
} satisfies Record<string, CSSProperties>;

const rules = {
    sidebar: css({ display: "none", background: "var(--panel)", "@supports (color: color-mix(in lab, red, red))": { background: "color-mix(in srgb, var(--panel) 94%, transparent)" }, "@media (width >= 64rem)": { display: "flex" } }),
    wordmark: css({ "& svg": { width: "1.5rem", height: "1.5rem" } }),
    nav: css({ "& > :not(:last-child)": { marginBottom: "0.25rem" } }),
    navItem: css({ color: "var(--muted)", "&:hover": { color: "var(--text)", background: "var(--panel-muted)" }, "& svg": { width: "1.25rem", height: "1.25rem" } }),
    accountSwitcher: css({ "& > svg": { width: "1rem", height: "1rem", color: "var(--muted)" } }),
    logout: css({ color: "var(--muted)", "&:hover": { color: "var(--danger)", background: "color-mix(in srgb, var(--danger) 8%, transparent)" }, "& svg": { width: "1rem", height: "1rem" } }),
    main: css({ paddingBottom: "5rem", "@media (width >= 64rem)": { paddingBottom: 0 } }),
    mobileNav: css({ display: "flex", background: "var(--panel)", "@supports (color: color-mix(in lab, red, red))": { background: "color-mix(in srgb, var(--panel) 94%, transparent)" }, "@media (width >= 64rem)": { display: "none" }, "& svg": { width: "1.25rem", height: "1.25rem" } }),
};

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
        <div style={styles.layout}>
            <aside className={rules.sidebar} style={styles.sidebar}>
                <button className={rules.wordmark} onClick={() => onNavigate("/")} style={styles.wordmark} type="button">
                    <span style={styles.mark}>
                        <IconLeaf2 />
                    </span>
                    Salvia
                </button>
                <nav aria-label="メインナビゲーション" className={rules.nav}>
                    {navigation.map((item) => (
                        <button aria-current={page === item.page ? "page" : undefined} className={rules.navItem} key={item.page} onClick={() => onNavigate(item.path)} style={{ ...styles.navItem, ...(page === item.page ? styles.navItemActive : {}) }} type="button">
                            <item.icon />
                            {item.label}
                        </button>
                    ))}
                </nav>
                <Button onClick={onCompose} style={styles.compose}>
                    <IconPlus />
                    ノート
                </Button>
                <div className={rules.accountSwitcher} style={styles.accountSwitcher}>
                    <Avatar actor={selectedActor} size="small" />
                    <label style={styles.accountLabel}>
                        <span style={styles.accountName}>{session.display_name || session.username}</span>
                        <select aria-label="操作するActor" onChange={(event) => onActorChange(event.target.value)} style={styles.actorSelect} value={selectedActor.id}>
                            {actors.map((actor) => (
                                <option key={actor.id} value={actor.id}>
                                    @{actor.username}
                                </option>
                            ))}
                        </select>
                    </label>
                    <IconChevronDown />
                </div>
                <button className={rules.logout} onClick={onLogout} style={styles.logout} type="button">
                    <IconLogout />
                    ログアウト
                </button>
            </aside>
            <main className={rules.main} style={styles.main}>
                {children}
            </main>
            <nav aria-label="モバイルナビゲーション" className={rules.mobileNav} style={styles.mobileNav}>
                {navigation.slice(0, 4).map((item) => (
                    <button aria-current={page === item.page ? "page" : undefined} key={item.page} onClick={() => onNavigate(item.path)} style={{ ...styles.mobileItem, ...(page === item.page ? { color: "var(--accent-hover)" } : {}) }} type="button">
                        <item.icon />
                        <span>{item.label}</span>
                    </button>
                ))}
                <button aria-label="ノートを作成" onClick={onCompose} style={styles.mobileCompose} type="button">
                    <IconPlus />
                </button>
            </nav>
        </div>
    );
}
