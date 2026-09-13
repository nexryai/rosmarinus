import { type CSSProperties, type ReactNode, useCallback, useEffect, useState } from "react";

import { IconBan, IconUserCheck, IconUserPlus, IconUserX } from "@tabler/icons-react";

import { EmojiText } from "../components/EmojiText";
import { Avatar, Button, DividedList, Empty, ErrorBanner, Loading, PageHeader } from "../components/ui";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { Dropdown, type DropdownOption } from "../components/ui/Dropdown";
import { api } from "../lib/api";
import { css } from "../lib/css";
import { useIsMobile } from "../lib/responsive";
import type { Connection } from "../lib/schema";

type FollowRequestAction = "accept" | "accept_and_follow" | "reject" | "reject_and_block";
type MobileAction = "" | FollowRequestAction;
type PendingDecision = { action: FollowRequestAction; item: Connection };

const styles = {
    headerIcon: {
        width: "1.5rem",
        height: "1.5rem",
        marginLeft: "auto",
        color: "var(--accent-hover)",
    },
    request: {
        paddingBlock: "1.25rem",
        display: "flex",
        alignItems: "center",
        gap: "0.75rem",
    },
    identity: {
        minWidth: 0,
        flex: 1,
    },
    name: {
        display: "block",
        overflow: "hidden",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    handle: {
        display: "block",
        overflow: "hidden",
        color: "var(--muted)",
        fontSize: "0.875rem",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    actions: {
        display: "flex",
        flexWrap: "wrap",
        justifyContent: "flex-end",
        gap: "0.5rem",
    },
    mobileActions: {
        width: "calc(100% - 3rem)",
        marginLeft: "3rem",
    },
    actionLabel: {
        display: "inline-flex",
        alignItems: "center",
        gap: "0.5rem",
    },
    actionIcon: {
        width: "1.125rem",
        height: "1.125rem",
        flexShrink: 0,
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    request: css({
        paddingInline: "1.25rem",
        "@media (width >= 40rem)": {
            paddingInline: "1.75rem",
        },
        "@media (width < 64rem)": {
            flexWrap: "wrap",
        },
    }),
};

const actionLabel = (icon: ReactNode, label: string) => (
    <span style={styles.actionLabel}>
        {icon}
        {label}
    </span>
);

const mobileActionOptions: DropdownOption<MobileAction>[] = [
    { value: "", label: "対応を選ぶ", disabled: true },
    { value: "accept", label: actionLabel(<IconUserCheck aria-hidden="true" style={styles.actionIcon} />, "承認") },
    {
        value: "accept_and_follow",
        label: actionLabel(<IconUserPlus aria-hidden="true" style={styles.actionIcon} />, "承認してフォローバック"),
        description: "承認後、このユーザーへフォローを申請します",
    },
    { value: "reject", label: actionLabel(<IconUserX aria-hidden="true" style={styles.actionIcon} />, "拒否") },
    { value: "reject_and_block", label: actionLabel(<IconBan aria-hidden="true" style={styles.actionIcon} />, "拒否してブロック") },
];

const confirmation = (decision: PendingDecision) => {
    const name = decision.item.actor.name || decision.item.actor.username;
    const details: Record<FollowRequestAction, { title: string; body: string; confirmLabel: string }> = {
        accept: {
            title: "フォローリクエストを承認しますか？",
            body: `${name}からのフォローリクエストを承認します。`,
            confirmLabel: "承認",
        },
        accept_and_follow: {
            title: "承認してフォローバックしますか？",
            body: `${name}からのリクエストを承認し、このユーザーへのフォローも申請します。相手が承認制の場合は、承認を待つ必要があります。`,
            confirmLabel: "承認してフォローバック",
        },
        reject: {
            title: "フォローリクエストを拒否しますか？",
            body: `${name}からのフォローリクエストを拒否します。`,
            confirmLabel: "拒否",
        },
        reject_and_block: {
            title: "フォローを拒否してブロックしますか？",
            body: `${name}からのフォローリクエストを拒否し、今後のやり取りを制限します。`,
            confirmLabel: "拒否してブロック",
        },
    };
    return details[decision.action];
};

export function FollowRequestsPage({ actorID, csrf, onOpenProfile, refreshKey }: { actorID: string; csrf: string; onOpenProfile: (actorID: string) => void; refreshKey: number }) {
    const [items, setItems] = useState<Connection[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [busyID, setBusyID] = useState("");
    const [pendingDecision, setPendingDecision] = useState<PendingDecision>();
    const isMobile = useIsMobile();
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
        setPendingDecision(undefined);
        void load();
    }, [load, refreshKey]);
    const decide = async (decision: PendingDecision) => {
        const { action, item } = decision;
        const status = action === "reject_and_block" ? "rejected_and_blocked" : action === "reject" ? "rejected" : "accepted";
        let accepted = false;
        setBusyID(item.id);
        try {
            await api.decideFollowRequest(csrf, actorID, item.actor.id, status);
            accepted = status === "accepted";
            setItems((current) => current.filter((value) => value.id !== item.id));
            if (action === "accept_and_follow") await api.follow(csrf, actorID, item.actor.uri);
        } catch (reason) {
            const message = reason instanceof Error ? reason.message : "操作に失敗しました";
            setError(accepted && action === "accept_and_follow" ? `リクエストは承認しましたが、フォローバックに失敗しました: ${message}` : message);
        } finally {
            setBusyID("");
            setPendingDecision(undefined);
        }
    };
    const openDecision = (item: Connection, action: FollowRequestAction) => setPendingDecision({ action, item });
    const dialog = pendingDecision ? confirmation(pendingDecision) : undefined;
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
                            <Avatar actor={item.actor} onOpenProfile={onOpenProfile} />
                            <div style={styles.identity}>
                                <strong style={styles.name}>
                                    <EmojiText emojis={item.actor.emojis} text={item.actor.name || item.actor.username} />
                                </strong>
                                <span style={styles.handle}>@{item.actor.username}</span>
                            </div>
                            {isMobile ? (
                                <Dropdown
                                    label={`${item.actor.name || item.actor.username}への対応`}
                                    onChange={(action) => {
                                        if (action) openDecision(item, action);
                                    }}
                                    options={mobileActionOptions}
                                    style={styles.mobileActions}
                                    value=""
                                />
                            ) : (
                                <div style={styles.actions}>
                                    <Button disabled={busyID === item.id} onClick={() => openDecision(item, "accept")}>
                                        <IconUserCheck aria-hidden="true" />
                                        承認
                                    </Button>
                                    <Button disabled={busyID === item.id} onClick={() => openDecision(item, "accept_and_follow")}>
                                        <IconUserPlus aria-hidden="true" />
                                        承認してフォローバック
                                    </Button>
                                    <Button disabled={busyID === item.id} onClick={() => openDecision(item, "reject")} variant="ghost">
                                        <IconUserX aria-hidden="true" />
                                        拒否
                                    </Button>
                                    <Button disabled={busyID === item.id} onClick={() => openDecision(item, "reject_and_block")} variant="danger">
                                        <IconBan aria-hidden="true" />
                                        拒否してブロック
                                    </Button>
                                </div>
                            )}
                        </article>
                    ))}
                </DividedList>
            )}
            {pendingDecision && dialog && (
                <ConfirmDialog busy={busyID === pendingDecision.item.id} confirmLabel={dialog.confirmLabel} onCancel={() => setPendingDecision(undefined)} onConfirm={() => void decide(pendingDecision)} title={dialog.title}>
                    {dialog.body}
                </ConfirmDialog>
            )}
        </>
    );
}
