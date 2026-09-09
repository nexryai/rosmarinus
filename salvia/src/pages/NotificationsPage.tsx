import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconAt, IconBell, IconBellCheck, IconChartBar, IconMessageReply, IconRepeat, IconUserPlus } from "@tabler/icons-react";

import { CustomEmoji, EmojiText } from "../components/EmojiText";
import { Mfm } from "../components/Mfm";
import { Twemoji } from "../components/Twemoji";
import { Avatar, Button, DividedList, Empty, ErrorBanner, Loading, PageHeader } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { Notification } from "../lib/schema";

const notificationKinds = {
    followRequest: { color: "#36aed2", icon: IconUserPlus, message: "からフォローリクエストがあります" },
    reaction: { color: "#e99a0b", icon: IconBell, message: "がリアクションしました" },
    renote: { color: "#36b982", icon: IconRepeat, message: "がリノートしました" },
    reply: { color: "#4389e8", icon: IconMessageReply, message: "から返信がありました" },
    mention: { color: "#7f96a5", icon: IconAt, message: "があなたに言及しました" },
    pollEnded: { color: "#7f96a5", icon: IconChartBar, message: "のアンケートが終了しました" },
} as const;

const notificationDetails = (kind: string) => notificationKinds[kind as keyof typeof notificationKinds] ?? { color: "#7f96a5", icon: IconBell, message: "から通知が届きました" };

const relativeTime = (value: string) => {
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 60) return `${seconds}秒`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}分`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}時間`;
    return new Intl.DateTimeFormat("ja", { month: "short", day: "numeric" }).format(new Date(value));
};

const styles = {
    headerIcon: {
        width: "1.5rem",
        height: "1.5rem",
        marginLeft: "auto",
        color: "var(--accent-hover)",
    },
    tabs: {
        paddingBlock: "0.75rem",
        display: "flex",
        gap: "0.5rem",
        borderBottom: "1px solid var(--border)",
    },
    tab: {
        padding: "0.5rem 1rem",
        borderRadius: "9999px",
        color: "var(--muted)",
        fontSize: "0.875rem",
        fontWeight: 700,
    },
    tabActive: {
        color: "var(--accent-ink)",
        background: "var(--accent-soft)",
    },
    notification: {
        position: "relative",
        paddingBlock: "1rem",
        display: "flex",
        alignItems: "flex-start",
        gap: "0.625rem",
    },
    avatar: {
        position: "relative",
        flexShrink: 0,
        width: "2.75rem",
        height: "2.75rem",
    },
    kindIcon: {
        position: "absolute",
        right: "-0.125rem",
        bottom: "-0.125rem",
        width: "1.25rem",
        height: "1.25rem",
        display: "grid",
        placeItems: "center",
        border: "3px solid var(--panel)",
        borderRadius: "9999px",
        color: "white",
        pointerEvents: "none",
    },
    kindIconSvg: {
        width: "0.6875rem",
        height: "0.6875rem",
        strokeWidth: 2.5,
    },
    kindEmoji: {
        width: "100%",
        height: "100%",
        margin: 0,
        verticalAlign: "middle",
    },
    body: {
        minWidth: 0,
        flex: 1,
    },
    header: {
        display: "flex",
        alignItems: "baseline",
        gap: "0.375rem",
        minWidth: 0,
        lineHeight: 1.35,
    },
    actorName: {
        overflow: "hidden",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    message: {
        color: "var(--muted)",
        fontSize: "0.875rem",
    },
    time: {
        marginLeft: "auto",
        flexShrink: 0,
        color: "var(--muted)",
        fontSize: "0.75rem",
    },
    quote: {
        marginTop: "0.25rem",
        display: "-webkit-box",
        overflow: "hidden",
        WebkitBoxOrient: "vertical",
        WebkitLineClamp: 1,
        color: "var(--text)",
        fontSize: "0.875rem",
        opacity: 0.72,
    },
    context: {
        marginTop: "0.5rem",
        marginRight: "0.75rem",
        color: "var(--accent-hover)",
        background: "transparent",
        fontSize: "0.75rem",
        fontWeight: 700,
        textDecoration: "underline",
    },
    readButton: {
        minHeight: "1.75rem",
        paddingInline: "0.625rem",
        fontSize: "0.75rem",
    },
    unreadDot: {
        position: "absolute",
        top: "0.625rem",
        left: "0.625rem",
        width: "0.4375rem",
        height: "0.4375rem",
        borderRadius: "9999px",
        background: "var(--accent-hover)",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    tabs: css({
        paddingInline: "1.25rem",
        "@media (width >= 40rem)": {
            paddingInline: "1.75rem",
        },
    }),
    notification: css({
        paddingInline: "1.25rem",
        "@media (width >= 40rem)": {
            paddingInline: "1.75rem",
        },
    }),
    unread: css({
        background: "var(--accent-soft)",
        "@supports (color: color-mix(in lab, red, red))": {
            background: "color-mix(in srgb, var(--accent-soft) 42%, var(--panel))",
        },
    }),
};

export function NotificationsPage({ actorID, csrf, onActorChange, onOpenNote, onOpenProfile, refreshKey }: { actorID: string; csrf: string; onActorChange: (actorID: string) => void; onOpenNote: (noteID: string) => void; onOpenProfile: (actorID: string) => void; refreshKey: number }) {
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
                    {items.map((item) => {
                        const details = notificationDetails(item.kind);
                        const KindIcon = details.icon;
                        const sourceName = item.source?.name || item.source?.username || "Fediverse";
                        return (
                            <article className={`${rules.notification} ${!item.is_read ? rules.unread : ""}`} key={item.id} style={styles.notification}>
                                {!item.is_read && <span aria-hidden="true" style={styles.unreadDot} />}
                                <div style={styles.avatar}>
                                    <Avatar actor={item.source} onOpenProfile={onOpenProfile} />
                                    <span aria-label={details.message.replace(/^[がかの]/, "")} role="img" style={{ ...styles.kindIcon, background: details.color }}>
                                        {item.kind === "reaction" && item.reaction_emoji ? (
                                            <CustomEmoji emoji={item.reaction_emoji} label="" style={styles.kindEmoji} />
                                        ) : item.kind === "reaction" && item.reaction ? (
                                            <span aria-hidden="true">
                                                <Twemoji style={styles.kindEmoji} text={item.reaction} />
                                            </span>
                                        ) : (
                                            <KindIcon aria-hidden="true" style={styles.kindIconSvg} />
                                        )}
                                    </span>
                                </div>
                                <div style={styles.body}>
                                    <div style={styles.header}>
                                        <strong style={styles.actorName}>
                                            <EmojiText emojis={item.source?.emojis} text={sourceName} />
                                        </strong>
                                        <span style={styles.message}>{details.message}</span>
                                        <time dateTime={item.created_at} style={styles.time} title={new Date(item.created_at).toLocaleString("ja")}>
                                            {relativeTime(item.created_at)}
                                        </time>
                                    </div>
                                    {item.note?.text && (
                                        <blockquote style={styles.quote}>
                                            “<Mfm emojis={item.note.emojis} text={item.note.text} />”
                                        </blockquote>
                                    )}
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
                                </div>
                                {!item.is_read && (
                                    <Button disabled={busyID === item.id} onClick={() => void markRead(item)} style={styles.readButton} variant="secondary">
                                        既読
                                    </Button>
                                )}
                            </article>
                        );
                    })}
                </DividedList>
            )}
        </>
    );
}
