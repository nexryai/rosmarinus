import { type CSSProperties, type FormEvent, useEffect, useState } from "react";

import { IconPalette, IconPlus, IconTrash, IconUserCircle } from "@tabler/icons-react";

import { SecuritySettings } from "../components/settings/SecuritySettings";
import { Button, ErrorBanner, PageHeader, Tabs } from "../components/ui";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { Dropdown, type DropdownOption } from "../components/ui/Dropdown";
import { Switch } from "../components/ui/Switch";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { AccountSettings, Actor, ActorSettings, QueueStatus } from "../lib/schema";

type SettingsTab = "general" | "profile" | "actor" | "security" | "system";
const settingsTabs: { value: SettingsTab; label: string }[] = [
    { value: "general", label: "General" },
    { value: "profile", label: "Profile" },
    { value: "actor", label: "Actor" },
    { value: "security", label: "Security" },
    { value: "system", label: "System" },
];

const queueLabels: Record<string, string> = {
    inbox: "受信",
    deliver: "配信",
    system: "システム",
    "poll-ended": "投票終了",
    media: "メディア",
    metadata: "メタデータ",
    "account-delete": "アカウント削除",
};

const styles = {
    headerIcon: {
        width: "1.5rem",
        height: "1.5rem",
        marginLeft: "auto",
        color: "var(--accent-hover)",
    },
    stack: {
        background: "var(--page)",
    },
    success: {
        margin: "1rem",
        padding: "0.75rem 1rem",
        border: "1px solid #b7dfc5",
        borderRadius: "1rem",
        color: "#287a48",
        background: "#effaf2",
        fontSize: "0.875rem",
    },
    card: {
        border: "1px solid var(--border)",
        borderRadius: "1.5rem",
        background: "var(--panel)",
    },
    cardTitle: {
        marginBottom: "0.25rem",
        fontSize: "1.125rem",
        lineHeight: 1.556,
        fontWeight: 900,
    },
    cardText: {
        marginBottom: "1.25rem",
        color: "var(--muted)",
        fontSize: "0.875rem",
    },
    field: {
        display: "block",
    },
    fieldLabel: {
        display: "block",
        marginBottom: "0.375rem",
        fontSize: "0.875rem",
        fontWeight: 700,
    },
    input: {
        width: "100%",
        padding: "0.625rem 1rem",
        borderWidth: 1,
        borderStyle: "solid",
        borderRadius: "1rem",
        outline: "none",
        color: "var(--text)",
        transition: "border-color 150ms, background-color 150ms",
    },
    dropdownTrigger: {
        minHeight: "2.875rem",
        paddingInline: "1rem",
    },
    toggle: {
        marginTop: "1rem",
        display: "flex",
        alignItems: "center",
        gap: "0.75rem",
        fontSize: "0.875rem",
        fontWeight: 600,
    },
    settingsTitle: {
        marginBottom: "1.25rem",
        display: "flex",
        alignItems: "center",
        gap: "0.75rem",
    },
    settingsIcon: {
        width: "1.75rem",
        height: "1.75rem",
        color: "var(--accent-hover)",
    },
    settingsSubtitle: {
        color: "var(--muted)",
        fontSize: "0.75rem",
    },
    grid: {
        display: "grid",
        gap: "1rem",
    },
    preferences: {
        marginTop: "1.5rem",
        paddingTop: "1.25rem",
        display: "grid",
        gap: "0.75rem",
        borderTop: "1px solid var(--border)",
    },
    inlineForm: {
        display: "grid",
        gap: "0.75rem",
    },
    tabsScroller: { overflowX: "auto" },
    queueHeader: { display: "flex", alignItems: "center", justifyContent: "space-between", gap: "1rem", flexWrap: "wrap" },
    queueGrid: { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(7rem, 1fr))", gap: "0.75rem", marginBlock: "1rem" },
    queueMetric: { padding: "0.875rem", borderRadius: "1rem", background: "var(--panel-muted)" },
    queueValue: { display: "block", fontSize: "1.5rem", fontWeight: 800, fontVariantNumeric: "tabular-nums" },
    queueList: { display: "grid", gap: "0.75rem" },
    queueItem: { padding: "1rem", border: "1px solid var(--border)", borderRadius: "1rem" },
    queueItemTitle: { display: "flex", alignItems: "center", justifyContent: "space-between", gap: "0.5rem", fontWeight: 800 },
    queueDetail: { marginTop: "0.5rem", color: "var(--muted)", fontSize: "0.8125rem", lineHeight: 1.6 },
} satisfies Record<string, CSSProperties>;

const rules = {
    stack: css({
        padding: "1rem",
        "& > :not(:last-child)": {
            marginBottom: "1rem",
        },
        "@media (width >= 40rem)": {
            padding: "1.5rem",
        },
    }),
    card: css({
        padding: "1.25rem",
        "@media (width >= 40rem)": {
            padding: "1.5rem",
        },
    }),
    input: css({
        borderColor: "var(--border)",
        background: "var(--panel-muted)",
        "&:focus": {
            borderColor: "var(--accent-hover)",
            background: "var(--panel)",
        },
    }),
    grid: css({
        "@media (width >= 40rem)": {
            gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
        },
    }),
    fullField: css({
        "@media (width >= 40rem)": {
            gridColumn: "span 2",
        },
    }),
    preferences: css({
        "@media (width >= 40rem)": {
            gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
        },
    }),
    inlineForm: css({
        "@media (width >= 40rem)": {
            gridTemplateColumns: "1fr 1fr auto",
        },
    }),
    dangerCard: css({
        borderColor: "var(--danger)",
        "@supports (color: color-mix(in lab, red, red))": {
            borderColor: "color-mix(in srgb, var(--danger) 25%, var(--border))",
        },
    }),
};

const themeOptions = [
    { value: "yellow", label: "Rosemary Yellow", description: "あたたかな黄色の標準テーマ" },
    { value: "light", label: "ライト", description: "明るくニュートラルな配色" },
    { value: "dark", label: "ダーク", description: "暗い場所でも見やすい配色" },
    { value: "system", label: "システム", description: "端末の外観設定に合わせる" },
] satisfies DropdownOption<AccountSettings["theme"]>[];

const visibilityOptions = [
    { value: "public", label: "公開", description: "すべてのユーザーに公開" },
    { value: "home", label: "ホーム", description: "連合タイムラインに表示しない" },
    { value: "followers", label: "フォロワー", description: "フォロワーだけに公開" },
] satisfies DropdownOption<ActorSettings["default_visibility"]>[];

export function SettingsPage({ accountSettings, actors, csrf, onActorsChanged, onSettingsChanged, selectedActor }: { accountSettings: AccountSettings; actors: Actor[]; csrf: string; onActorsChanged: () => Promise<void>; onSettingsChanged: (value: AccountSettings) => void; selectedActor: Actor }) {
    const [tab, setTab] = useState<SettingsTab>("general");
    const [queueStatus, setQueueStatus] = useState<QueueStatus>();
    const [queueError, setQueueError] = useState("");
    const [queueLoading, setQueueLoading] = useState(false);
    const [queueRefresh, setQueueRefresh] = useState(0);
    const [actorSettings, setActorSettings] = useState<ActorSettings>();
    const [name, setName] = useState(selectedActor.name);
    const [summary, setSummary] = useState(selectedActor.summary);
    const [avatarURL, setAvatarURL] = useState(selectedActor.avatar_url);
    const [newUsername, setNewUsername] = useState("");
    const [newName, setNewName] = useState("");
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
    const [deleting, setDeleting] = useState(false);
    useEffect(() => {
        setDeleteDialogOpen(false);
        setName(selectedActor.name);
        setSummary(selectedActor.summary);
        setAvatarURL(selectedActor.avatar_url);
        api.actorSettings(selectedActor.id)
            .then(setActorSettings)
            .catch((reason) => setError(reason instanceof Error ? reason.message : "Actor設定を読み込めませんでした"));
    }, [selectedActor.avatar_url, selectedActor.id, selectedActor.name, selectedActor.summary]);
    // biome-ignore lint/correctness/useExhaustiveDependencies: the refresh counter intentionally triggers a new queue snapshot.
    useEffect(() => {
        if (tab !== "system") return;
        const controller = new AbortController();
        let current = true;
        const load = async () => {
            setQueueLoading(true);
            try {
                const status = await api.queueStatus(controller.signal);
                if (current) {
                    setQueueStatus(status);
                    setQueueError("");
                }
            } catch (reason) {
                if (current) setQueueError(reason instanceof Error ? reason.message : "ジョブキューを読み込めませんでした");
            } finally {
                if (current) setQueueLoading(false);
            }
        };
        void load();
        return () => {
            current = false;
            controller.abort();
        };
    }, [tab, queueRefresh]);
    const queueTotals = queueStatus?.queues.reduce(
        (totals, item) => ({
            active: totals.active + item.active,
            retry: totals.retry + item.retry,
            pending: totals.pending + item.pending,
            scheduled: totals.scheduled + item.scheduled,
            archived: totals.archived + item.archived,
            completed: totals.completed + item.completed,
            aggregating: totals.aggregating + item.aggregating,
        }),
        { active: 0, retry: 0, pending: 0, scheduled: 0, archived: 0, completed: 0, aggregating: 0 },
    );
    const updateAccount = async (patch: Partial<AccountSettings>) => {
        setError("");
        try {
            const value = await api.updateAccountSettings(csrf, patch);
            onSettingsChanged(value);
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "設定を保存できませんでした");
        }
    };
    const saveProfile = async (event: FormEvent) => {
        event.preventDefault();
        setError("");
        try {
            await api.updateActor(csrf, selectedActor.id, { name, summary, avatar_url: avatarURL });
            await onActorsChanged();
            setMessage("プロフィールを保存しました");
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "プロフィールを保存できませんでした");
        }
    };
    const saveActorSettings = async (patch: Partial<ActorSettings>) => {
        try {
            setActorSettings(await api.updateActorSettings(csrf, selectedActor.id, patch));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "Actor設定を保存できませんでした");
        }
    };
    const createActor = async (event: FormEvent) => {
        event.preventDefault();
        try {
            await api.createActor(csrf, newUsername.trim(), newName.trim());
            setNewUsername("");
            setNewName("");
            await onActorsChanged();
            setMessage("Actorを作成しました");
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "Actorを作成できませんでした");
        }
    };
    const deleteActor = async () => {
        setDeleting(true);
        setError("");
        try {
            await api.deleteActor(csrf, selectedActor.id);
            setDeleteDialogOpen(false);
            await onActorsChanged();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "削除できませんでした");
        } finally {
            setDeleting(false);
        }
    };
    return (
        <>
            <PageHeader title="設定" trailing={<IconPalette style={styles.headerIcon} />} />
            <div className={rules.stack} style={styles.stack}>
                <div style={styles.tabsScroller}>
                    <Tabs ariaLabel="設定カテゴリー" items={settingsTabs} onChange={setTab} style={{ minWidth: "max-content" }} value={tab} variant="underline" />
                </div>
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                {message && (
                    <p role="status" style={styles.success}>
                        {message}
                    </p>
                )}
                {tab === "general" && (
                    <section className={rules.card} style={styles.card}>
                        <h2 style={styles.cardTitle}>表示</h2>
                        <div style={styles.field}>
                            <span style={styles.fieldLabel}>テーマ</span>
                            <Dropdown label="テーマ" onChange={(theme) => void updateAccount({ theme })} options={themeOptions} triggerStyle={styles.dropdownTrigger} value={accountSettings.theme} />
                        </div>
                        <Switch checked={accountSettings.reduce_motion} label="動きを減らす" onChange={(reduce_motion) => void updateAccount({ reduce_motion })} style={styles.toggle} />
                        <Switch checked={accountSettings.compact_mode} label="コンパクト表示" onChange={(compact_mode) => void updateAccount({ compact_mode })} style={styles.toggle} />
                    </section>
                )}
                {tab === "profile" && (
                    <section className={rules.card} style={styles.card}>
                        <div style={styles.settingsTitle}>
                            <IconUserCircle style={styles.settingsIcon} />
                            <div>
                                <h2 style={{ ...styles.cardTitle, marginBottom: 0 }}>@{selectedActor.username}</h2>
                                <p style={styles.settingsSubtitle}>公開プロフィール</p>
                            </div>
                        </div>
                        <form className={rules.grid} onSubmit={saveProfile} style={styles.grid}>
                            <label style={styles.field}>
                                <span style={styles.fieldLabel}>表示名</span>
                                <input className={rules.input} maxLength={128} onChange={(event) => setName(event.target.value)} style={styles.input} value={name} />
                            </label>
                            <label style={styles.field}>
                                <span style={styles.fieldLabel}>アバターURL</span>
                                <input className={rules.input} inputMode="url" onChange={(event) => setAvatarURL(event.target.value)} placeholder="https://…" style={styles.input} value={avatarURL} />
                            </label>
                            <label className={rules.fullField} style={styles.field}>
                                <span style={styles.fieldLabel}>自己紹介</span>
                                <textarea className={rules.input} maxLength={1500} onChange={(event) => setSummary(event.target.value)} rows={4} style={styles.input} value={summary} />
                            </label>
                            <Button type="submit">プロフィールを保存</Button>
                        </form>
                    </section>
                )}
                {tab === "actor" && (
                    <>
                        <section className={rules.card} style={styles.card}>
                            <h2 style={styles.cardTitle}>@{selectedActor.username} の設定</h2>
                            {actorSettings ? (
                                <div className={rules.preferences} style={styles.preferences}>
                                    <div style={styles.field}>
                                        <span style={styles.fieldLabel}>標準の公開範囲</span>
                                        <Dropdown label="標準の公開範囲" onChange={(default_visibility) => void saveActorSettings({ default_visibility })} options={visibilityOptions} triggerStyle={styles.dropdownTrigger} value={actorSettings.default_visibility} />
                                    </div>
                                    <Switch checked={actorSettings.pinned} label="Actor切替で上に固定" onChange={(pinned) => void saveActorSettings({ pinned })} style={styles.toggle} />
                                    <label style={styles.field}>
                                        <span style={styles.fieldLabel}>表示色</span>
                                        <input
                                            className={rules.input}
                                            onBlur={(event) => void saveActorSettings({ color: event.target.value })}
                                            type="color"
                                            value={actorSettings.color || "#e9a91d"}
                                            onChange={(event) => setActorSettings((current) => (current ? { ...current, color: event.target.value } : current))}
                                            style={styles.input}
                                        />
                                    </label>
                                    <label style={styles.field}>
                                        <span style={styles.fieldLabel}>表示順</span>
                                        <input
                                            className={rules.input}
                                            min={0}
                                            onBlur={(event) => void saveActorSettings({ display_order: Number(event.target.value) })}
                                            type="number"
                                            value={actorSettings.display_order}
                                            onChange={(event) => setActorSettings((current) => (current ? { ...current, display_order: Number(event.target.value) } : current))}
                                            style={styles.input}
                                        />
                                    </label>
                                    <Switch checked={actorSettings.show_content_warning} label="CW本文を初期表示する" onChange={(show_content_warning) => void saveActorSettings({ show_content_warning })} style={styles.toggle} />
                                </div>
                            ) : (
                                <p style={styles.cardText}>Actor設定を読み込み中…</p>
                            )}
                        </section>
                        <section className={rules.card} style={styles.card}>
                            <h2 style={styles.cardTitle}>Actorを追加</h2>
                            <p style={styles.cardText}>ひとつのアカウントで複数の公開アイデンティティを管理できます。</p>
                            <form className={rules.inlineForm} onSubmit={createActor} style={styles.inlineForm}>
                                <input aria-label="新しいActorのユーザー名" className={rules.input} maxLength={64} onChange={(event) => setNewUsername(event.target.value)} placeholder="username" required style={styles.input} value={newUsername} />
                                <input aria-label="新しいActorの表示名" className={rules.input} maxLength={128} onChange={(event) => setNewName(event.target.value)} placeholder="表示名" style={styles.input} value={newName} />
                                <Button type="submit">
                                    <IconPlus />
                                    追加
                                </Button>
                            </form>
                        </section>
                        {actors.length > 1 && (
                            <section className={`${rules.card} ${rules.dangerCard}`} style={styles.card}>
                                <h2 style={styles.cardTitle}>Actorを削除</h2>
                                <p style={styles.cardText}>@{selectedActor.username}を削除すると元に戻せません。</p>
                                <Button onClick={() => setDeleteDialogOpen(true)} variant="danger">
                                    <IconTrash />
                                    このActorを削除
                                </Button>
                            </section>
                        )}
                    </>
                )}
                {tab === "system" && (
                    <section className={rules.card} style={styles.card}>
                        <div style={styles.queueHeader}>
                            <div>
                                <h2 style={styles.cardTitle}>ジョブキュー</h2>
                                <p style={styles.cardText}>各キューの現在のジョブ数を表示します。</p>
                            </div>
                            <Button disabled={queueLoading} onClick={() => setQueueRefresh((value) => value + 1)} size="small" type="button">
                                更新
                            </Button>
                        </div>
                        {queueError && <ErrorBanner message={queueError} onDismiss={() => setQueueError("")} />}
                        {queueStatus ? (
                            <>
                                <p style={styles.settingsSubtitle}>取得時刻: {new Date(queueStatus.updated_at).toLocaleString()}</p>
                                <div style={styles.queueGrid}>
                                    {(
                                        [
                                            ["実行中", queueTotals?.active],
                                            ["再試行待ち", queueTotals?.retry],
                                            ["待機中", queueTotals?.pending],
                                            ["予約済み", queueTotals?.scheduled],
                                            ["失敗・保管", queueTotals?.archived],
                                            ["集約中", queueTotals?.aggregating],
                                            ["完了（保存分）", queueTotals?.completed],
                                        ] as const
                                    ).map(([label, count]) => (
                                        <div key={label} style={styles.queueMetric}>
                                            <span>{label}</span>
                                            <strong style={styles.queueValue}>{count ?? 0}</strong>
                                        </div>
                                    ))}
                                </div>
                                <div style={styles.queueList}>
                                    {queueStatus.queues.map((item) => (
                                        <div key={item.name} style={styles.queueItem}>
                                            <div style={styles.queueItemTitle}>
                                                <span>
                                                    {queueLabels[item.name] ?? item.name} <small style={styles.settingsSubtitle}>({item.name})</small>
                                                </span>
                                                {item.paused && <span>停止中</span>}
                                            </div>
                                            <p style={styles.queueDetail}>
                                                実行中 {item.active} · 再試行待ち {item.retry} · 待機中 {item.pending} · 予約済み {item.scheduled} · 失敗・保管 {item.archived} · 集約中 {item.aggregating} · 完了（保存分） {item.completed}
                                            </p>
                                        </div>
                                    ))}
                                </div>
                            </>
                        ) : (
                            !queueError && (
                                <p role="status" style={styles.cardText}>
                                    ジョブキューを読み込み中…
                                </p>
                            )
                        )}
                    </section>
                )}
                {tab === "security" && <SecuritySettings csrf={csrf} />}
            </div>
            {deleteDialogOpen && (
                <ConfirmDialog busy={deleting} confirmLabel="削除する" onCancel={() => setDeleteDialogOpen(false)} onConfirm={() => void deleteActor()} title="Actorを削除しますか？">
                    @{selectedActor.username}を削除すると元に戻せません。
                </ConfirmDialog>
            )}
        </>
    );
}
