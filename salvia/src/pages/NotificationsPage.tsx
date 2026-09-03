import { useCallback, useEffect, useState } from "react";

import { IconBellCheck } from "@tabler/icons-react";

import { Avatar, Button, Empty, ErrorBanner, Loading } from "../components/ui";
import { api } from "../lib/api";
import type { Notification } from "../lib/schema";

const labels: Record<string, string> = { follow: "フォローされました", reaction: "リアクションが届きました", mention: "メンションされました", reply: "返信が届きました", poll_vote: "投票されました" };

export function NotificationsPage({ actorID, csrf, refreshKey }: { actorID: string; csrf: string; refreshKey: number }) {
    const [items, setItems] = useState<Notification[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        try {
            setItems(await api.notifications(actorID));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "通知を読み込めませんでした");
        } finally {
            setLoading(false);
        }
    }, [actorID]);
    useEffect(() => {
        void refreshKey;
        void load();
    }, [load, refreshKey]);
    const markRead = async (item: Notification) => {
        if (item.is_read) return;
        try {
            await api.markNotificationRead(csrf, actorID, item.id);
            setItems((current) => current.map((value) => (value.id === item.id ? { ...value, is_read: true } : value)));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "既読にできませんでした");
        }
    };
    return (
        <>
            <header className="page-header">
                <div>
                    <p className="eyebrow">最新の動き</p>
                    <h1>通知</h1>
                </div>
                <IconBellCheck className="header-icon" />
            </header>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading ? (
                <Loading />
            ) : items.length === 0 ? (
                <Empty>新しい通知はありません。</Empty>
            ) : (
                <section className="notification-list">
                    {items.map((item) => (
                        <article className={item.is_read ? "notification" : "notification notification--unread"} key={item.id}>
                            <Avatar actor={item.source} />
                            <div>
                                <strong>{item.source?.name || item.source?.username || "Fediverse"}</strong>
                                <p>{labels[item.kind] || item.kind}</p>
                                {item.note?.text && <blockquote>{item.note.text}</blockquote>}
                                <time>{new Intl.DateTimeFormat("ja", { dateStyle: "medium", timeStyle: "short" }).format(new Date(item.created_at))}</time>
                            </div>
                            {!item.is_read && (
                                <Button onClick={() => void markRead(item)} variant="secondary">
                                    既読
                                </Button>
                            )}
                        </article>
                    ))}
                </section>
            )}
        </>
    );
}
