import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconBellCheck } from "@tabler/icons-react";

import { Avatar, Button, DividedList, Empty, ErrorBanner, Loading, PageHeader } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { Notification } from "../lib/schema";

const labels: Record<string, string> = { follow: "フォローされました", reaction: "リアクションが届きました", mention: "メンションされました", reply: "返信が届きました", poll_vote: "投票されました" };

const styles = {
    headerIcon: { width: "1.5rem", height: "1.5rem", marginLeft: "auto", color: "var(--accent-hover)" },
    tabs: { paddingBlock: "0.75rem", display: "flex", gap: "0.5rem", borderBottom: "1px solid var(--border)" },
    tab: { padding: "0.5rem 1rem", borderRadius: "9999px", color: "var(--muted)", fontSize: "0.875rem", fontWeight: 700 },
    tabActive: { color: "var(--accent-ink)", background: "var(--accent-soft)" },
    notification: { paddingBlock: "1.25rem", display: "flex", alignItems: "flex-start", gap: "0.75rem" },
    body: { minWidth: 0, flex: 1 },
    kind: { color: "var(--muted)", fontSize: "0.875rem" },
    quote: { marginTop: "0.5rem", padding: "0.75rem", display: "-webkit-box", overflow: "hidden", WebkitBoxOrient: "vertical", WebkitLineClamp: 2, borderRadius: "0.75rem", background: "var(--panel-muted)", fontSize: "0.875rem" },
    context: { marginTop: "0.5rem", marginRight: "0.75rem", color: "var(--accent-hover)", background: "transparent", fontSize: "0.75rem", fontWeight: 700, textDecoration: "underline" },
    time: { marginTop: "0.5rem", display: "block", color: "var(--muted)", fontSize: "0.75rem" },
    readButton: { minHeight: "2rem", paddingInline: "0.75rem", fontSize: "0.75rem" },
} satisfies Record<string, CSSProperties>;

const rules = {
    tabs: css({ paddingInline: "1.25rem", "@media (width >= 40rem)": { paddingInline: "1.75rem" } }),
    notification: css({ paddingInline: "1.25rem", "@media (width >= 40rem)": { paddingInline: "1.75rem" } }),
    unread: css({ background: "var(--accent-soft)", "@supports (color: color-mix(in lab, red, red))": { background: "color-mix(in srgb, var(--accent-soft) 42%, var(--panel))" } }),
};

export function NotificationsPage({ actorID, csrf, onActorChange, onOpenNote, refreshKey }: { actorID: string; csrf: string; onActorChange: (actorID: string) => void; onOpenNote: (noteID: string) => void; refreshKey: number }) {
    const [items, setItems] = useState<Notification[]>([]);
    const [scope, setScope] = useState<"actor" | "account">("actor");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [busyID, setBusyID] = useState("");
    const load = useCallback(
        async (signal?: AbortSignal) => {
            setLoading(true);
            try {
                setItems(scope === "actor" ? await api.notifications(actorID, signal) : await api.accountNotifications(signal));
            } catch (reason) {
                if (!signal?.aborted) setError(reason instanceof Error ? reason.message : "通知を読み込めませんでした");
            } finally {
                if (!signal?.aborted) setLoading(false);
            }
        },
        [actorID, scope],
    );
    useEffect(() => {
        void refreshKey;
        const controller = new AbortController();
        void load(controller.signal);
        return () => controller.abort();
    }, [load, refreshKey]);
    const markRead = async (item: Notification) => {
        if (item.is_read) return;
        setBusyID(item.id);
        try {
            await api.markNotificationRead(csrf, actorID, item.id);
            setItems((current) => current.map((value) => (value.id === item.id ? { ...value, is_read: true } : value)));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "既読にできませんでした");
        } finally {
            setBusyID("");
        }
    };
    return (
        <>
            <PageHeader eyebrow="最新の動き" title="通知" trailing={<IconBellCheck style={styles.headerIcon} />} />
            <div className={rules.tabs} style={styles.tabs}>
                <button aria-pressed={scope === "actor"} onClick={() => setScope("actor")} style={{ ...styles.tab, ...(scope === "actor" ? styles.tabActive : {}) }} type="button">
                    このActor
                </button>
                <button aria-pressed={scope === "account"} onClick={() => setScope("account")} style={{ ...styles.tab, ...(scope === "account" ? styles.tabActive : {}) }} type="button">
                    すべてのActor
                </button>
            </div>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading ? (
                <Loading />
            ) : items.length === 0 ? (
                <Empty>新しい通知はありません。</Empty>
            ) : (
                <DividedList>
                    {items.map((item) => (
                        <article className={`${rules.notification} ${!item.is_read ? rules.unread : ""}`} key={item.id} style={styles.notification}>
                            <Avatar actor={item.source} />
                            <div style={styles.body}>
                                <strong>{item.source?.name || item.source?.username || "Fediverse"}</strong>
                                <p style={styles.kind}>{labels[item.kind] || item.kind}</p>
                                {item.note?.text && <blockquote style={styles.quote}>{item.note.text}</blockquote>}
                                {scope === "account" && item.actor_id !== actorID && (
                                    <button onClick={() => onActorChange(item.actor_id)} style={styles.context} type="button">
                                        この通知のActorへ切り替え
                                    </button>
                                )}
                                {item.note_id && (
                                    <button onClick={() => onOpenNote(item.note_id || "")} style={styles.context} type="button">
                                        ノートを開く
                                    </button>
                                )}
                                <time style={styles.time}>{new Intl.DateTimeFormat("ja", { dateStyle: "medium", timeStyle: "short" }).format(new Date(item.created_at))}</time>
                            </div>
                            {!item.is_read && (
                                <Button disabled={busyID === item.id} onClick={() => void markRead(item)} style={styles.readButton} variant="secondary">
                                    既読
                                </Button>
                            )}
                        </article>
                    ))}
                </DividedList>
            )}
        </>
    );
}
