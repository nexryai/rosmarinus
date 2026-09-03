import { useCallback, useEffect, useState } from "react";

import { IconUserCheck } from "@tabler/icons-react";

import { Avatar, Button, Empty, ErrorBanner, Loading } from "../components/ui";
import { api } from "../lib/api";
import type { Connection } from "../lib/schema";

export function FollowRequestsPage({ actorID, csrf, refreshKey }: { actorID: string; csrf: string; refreshKey: number }) {
    const [items, setItems] = useState<Connection[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        try {
            setItems(await api.followRequests(actorID));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "リクエストを読み込めませんでした");
        } finally {
            setLoading(false);
        }
    }, [actorID]);
    useEffect(() => {
        void refreshKey;
        void load();
    }, [load, refreshKey]);
    const decide = async (item: Connection, status: "accepted" | "rejected") => {
        try {
            await api.decideFollowRequest(csrf, actorID, item.actor.id, status);
            setItems((current) => current.filter((value) => value.id !== item.id));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "操作に失敗しました");
        }
    };
    return (
        <>
            <header className="page-header">
                <div>
                    <p className="eyebrow">承認制</p>
                    <h1>フォローリクエスト</h1>
                </div>
                <IconUserCheck className="header-icon" />
            </header>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading ? (
                <Loading />
            ) : items.length === 0 ? (
                <Empty>保留中のリクエストはありません。</Empty>
            ) : (
                <section className="request-list">
                    {items.map((item) => (
                        <article className="request" key={item.id}>
                            <Avatar actor={item.actor} />
                            <div>
                                <strong>{item.actor.name || item.actor.username}</strong>
                                <span>@{item.actor.username}</span>
                            </div>
                            <div className="request__actions">
                                <Button onClick={() => void decide(item, "accepted")}>承認</Button>
                                <Button onClick={() => void decide(item, "rejected")} variant="ghost">
                                    拒否
                                </Button>
                            </div>
                        </article>
                    ))}
                </section>
            )}
        </>
    );
}
