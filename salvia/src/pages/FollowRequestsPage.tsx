import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconUserCheck } from "@tabler/icons-react";

import { Avatar, Button, DividedList, Empty, ErrorBanner, Loading, PageHeader } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { Connection } from "../lib/schema";

const styles = {
    headerIcon: { width: "1.5rem", height: "1.5rem", marginLeft: "auto", color: "var(--accent-hover)" },
    request: { paddingBlock: "1.25rem", display: "flex", alignItems: "center", gap: "0.75rem" },
    identity: { minWidth: 0, flex: 1 },
    name: { display: "block", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
    handle: { display: "block", overflow: "hidden", color: "var(--muted)", fontSize: "0.875rem", textOverflow: "ellipsis", whiteSpace: "nowrap" },
    actions: { display: "flex", gap: "0.5rem" },
} satisfies Record<string, CSSProperties>;

const rules = {
    request: css({ paddingInline: "1.25rem", "@media (width >= 40rem)": { paddingInline: "1.75rem" }, "@media (width <= 639px)": { flexWrap: "wrap" } }),
    actions: css({ "@media (width <= 639px)": { width: "100%", marginLeft: "3rem" } }),
};

export function FollowRequestsPage({ actorID, csrf, refreshKey }: { actorID: string; csrf: string; refreshKey: number }) {
    const [items, setItems] = useState<Connection[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [busyID, setBusyID] = useState("");
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
        setBusyID(item.id);
        try {
            await api.decideFollowRequest(csrf, actorID, item.actor.id, status);
            setItems((current) => current.filter((value) => value.id !== item.id));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "操作に失敗しました");
        } finally {
            setBusyID("");
        }
    };
    return (
        <>
            <PageHeader eyebrow="承認制" title="フォローリクエスト" trailing={<IconUserCheck style={styles.headerIcon} />} />
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading ? (
                <Loading />
            ) : items.length === 0 ? (
                <Empty>保留中のリクエストはありません。</Empty>
            ) : (
                <DividedList>
                    {items.map((item) => (
                        <article className={rules.request} key={item.id} style={styles.request}>
                            <Avatar actor={item.actor} />
                            <div style={styles.identity}>
                                <strong style={styles.name}>{item.actor.name || item.actor.username}</strong>
                                <span style={styles.handle}>@{item.actor.username}</span>
                            </div>
                            <div className={rules.actions} style={styles.actions}>
                                <Button disabled={busyID === item.id} onClick={() => void decide(item, "accepted")}>
                                    承認
                                </Button>
                                <Button disabled={busyID === item.id} onClick={() => void decide(item, "rejected")} variant="ghost">
                                    拒否
                                </Button>
                            </div>
                        </article>
                    ))}
                </DividedList>
            )}
        </>
    );
}
