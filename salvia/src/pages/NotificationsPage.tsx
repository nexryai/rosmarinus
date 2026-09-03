import { useCallback, useEffect, useState } from "react";

import { IconBellCheck } from "@tabler/icons-react";

import { Avatar, Button, Empty, ErrorBanner, Loading } from "../components/ui";
import { api } from "../lib/api";
import type { Notification } from "../lib/schema";

const labels: Record<string, string> = { follow: "フォローされました", reaction: "リアクションが届きました", mention: "メンションされました", reply: "返信が届きました", poll_vote: "投票されました" };

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
            <header className="page-header">
                <div>
                    <p className="eyebrow">最新の動き</p>
                    <h1>通知</h1>
                </div>
                <IconBellCheck className="header-icon" />
            </header>
            <div className="scope-tabs">
                <button aria-pressed={scope === "actor"} onClick={() => setScope("actor")} type="button">
                    このActor
                </button>
                <button aria-pressed={scope === "account"} onClick={() => setScope("account")} type="button">
                    すべてのActor
                </button>
            </div>
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
                                {scope === "account" && item.actor_id !== actorID && (
                                    <button className="notification__context" onClick={() => onActorChange(item.actor_id)} type="button">
                                        この通知のActorへ切り替え
                                    </button>
                                )}
                                {item.note_id && (
                                    <button className="notification__context" onClick={() => onOpenNote(item.note_id || "")} type="button">
                                        ノートを開く
                                    </button>
                                )}
                                <time>{new Intl.DateTimeFormat("ja", { dateStyle: "medium", timeStyle: "short" }).format(new Date(item.created_at))}</time>
                            </div>
                            {!item.is_read && (
                                <Button disabled={busyID === item.id} onClick={() => void markRead(item)} variant="secondary">
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
