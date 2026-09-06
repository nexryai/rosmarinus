import { type CSSProperties, type FormEvent, useEffect, useState } from "react";

import { IconPalette, IconPlus, IconTrash, IconUserCircle } from "@tabler/icons-react";

import { Button, ErrorBanner, PageHeader } from "../components/ui";
import { Dropdown, type DropdownOption } from "../components/ui/Dropdown";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { AccountSettings, Actor, ActorSettings } from "../lib/schema";

const styles = {
    headerIcon: { width: "1.5rem", height: "1.5rem", marginLeft: "auto", color: "var(--accent-hover)" },
    stack: { background: "var(--page)" },
    success: { margin: "1rem", padding: "0.75rem 1rem", border: "1px solid #b7dfc5", borderRadius: "1rem", color: "#287a48", background: "#effaf2", fontSize: "0.875rem" },
    card: { border: "1px solid var(--border)", borderRadius: "1.5rem", background: "var(--panel)" },
    cardTitle: { marginBottom: "0.25rem", fontSize: "1.125rem", lineHeight: 1.556, fontWeight: 900 },
    cardText: { marginBottom: "1.25rem", color: "var(--muted)", fontSize: "0.875rem" },
    field: { display: "block" },
    fieldLabel: { display: "block", marginBottom: "0.375rem", fontSize: "0.875rem", fontWeight: 700 },
    input: { width: "100%", padding: "0.625rem 1rem", borderWidth: 1, borderStyle: "solid", borderRadius: "1rem", outline: "none", color: "var(--text)", transition: "border-color 150ms, background-color 150ms" },
    dropdownTrigger: { minHeight: "2.875rem", paddingInline: "1rem" },
    toggle: { marginTop: "1rem", display: "flex", alignItems: "center", gap: "0.75rem", fontSize: "0.875rem", fontWeight: 600 },
    checkbox: { width: "1rem", height: "1rem", accentColor: "var(--accent-hover)" },
    settingsTitle: { marginBottom: "1.25rem", display: "flex", alignItems: "center", gap: "0.75rem" },
    settingsIcon: { width: "1.75rem", height: "1.75rem", color: "var(--accent-hover)" },
    settingsSubtitle: { color: "var(--muted)", fontSize: "0.75rem" },
    grid: { display: "grid", gap: "1rem" },
    preferences: { marginTop: "1.5rem", paddingTop: "1.25rem", display: "grid", gap: "0.75rem", borderTop: "1px solid var(--border)" },
    inlineForm: { display: "grid", gap: "0.75rem" },
} satisfies Record<string, CSSProperties>;

const rules = {
    stack: css({ padding: "1rem", "& > :not(:last-child)": { marginBottom: "1rem" }, "@media (width >= 40rem)": { padding: "1.5rem" } }),
    card: css({ padding: "1.25rem", "@media (width >= 40rem)": { padding: "1.5rem" } }),
    input: css({ borderColor: "var(--border)", background: "var(--panel-muted)", "&:focus": { borderColor: "var(--accent-hover)", background: "var(--panel)" } }),
    grid: css({ "@media (width >= 40rem)": { gridTemplateColumns: "repeat(2, minmax(0, 1fr))" } }),
    fullField: css({ "@media (width >= 40rem)": { gridColumn: "span 2" } }),
    preferences: css({ "@media (width >= 40rem)": { gridTemplateColumns: "repeat(2, minmax(0, 1fr))" } }),
    inlineForm: css({ "@media (width >= 40rem)": { gridTemplateColumns: "1fr 1fr auto" } }),
    dangerCard: css({ borderColor: "var(--danger)", "@supports (color: color-mix(in lab, red, red))": { borderColor: "color-mix(in srgb, var(--danger) 25%, var(--border))" } }),
};

const themeOptions = [
    { value: "yellow", label: "Salvia Yellow", description: "あたたかな黄色の標準テーマ" },
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
    const [actorSettings, setActorSettings] = useState<ActorSettings>();
    const [name, setName] = useState(selectedActor.name);
    const [summary, setSummary] = useState(selectedActor.summary);
    const [avatarURL, setAvatarURL] = useState(selectedActor.avatar_url);
    const [newUsername, setNewUsername] = useState("");
    const [newName, setNewName] = useState("");
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    useEffect(() => {
        setName(selectedActor.name);
        setSummary(selectedActor.summary);
        setAvatarURL(selectedActor.avatar_url);
        api.actorSettings(selectedActor.id)
            .then(setActorSettings)
            .catch((reason) => setError(reason instanceof Error ? reason.message : "Actor設定を読み込めませんでした"));
    }, [selectedActor.avatar_url, selectedActor.id, selectedActor.name, selectedActor.summary]);
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
    return (
        <>
            <PageHeader eyebrow="アカウントとActor" title="設定" trailing={<IconPalette style={styles.headerIcon} />} />
            <div className={rules.stack} style={styles.stack}>
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                {message && (
                    <p role="status" style={styles.success}>
                        {message}
                    </p>
                )}
                <section className={rules.card} style={styles.card}>
                    <h2 style={styles.cardTitle}>表示</h2>
                    <div style={styles.field}>
                        <span style={styles.fieldLabel}>テーマ</span>
                        <Dropdown label="テーマ" onChange={(theme) => void updateAccount({ theme })} options={themeOptions} triggerStyle={styles.dropdownTrigger} value={accountSettings.theme} />
                    </div>
                    <label style={styles.toggle}>
                        <input checked={accountSettings.reduce_motion} onChange={(event) => void updateAccount({ reduce_motion: event.target.checked })} style={styles.checkbox} type="checkbox" />
                        <span>動きを減らす</span>
                    </label>
                    <label style={styles.toggle}>
                        <input checked={accountSettings.compact_mode} onChange={(event) => void updateAccount({ compact_mode: event.target.checked })} style={styles.checkbox} type="checkbox" />
                        <span>コンパクト表示</span>
                    </label>
                </section>
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
                    {actorSettings && (
                        <div className={rules.preferences} style={styles.preferences}>
                            <div style={styles.field}>
                                <span style={styles.fieldLabel}>標準の公開範囲</span>
                                <Dropdown label="標準の公開範囲" onChange={(default_visibility) => void saveActorSettings({ default_visibility })} options={visibilityOptions} triggerStyle={styles.dropdownTrigger} value={actorSettings.default_visibility} />
                            </div>
                            <label style={styles.toggle}>
                                <input checked={actorSettings.pinned} onChange={(event) => void saveActorSettings({ pinned: event.target.checked })} style={styles.checkbox} type="checkbox" />
                                <span>Actor切替で上に固定</span>
                            </label>
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
                            <label style={styles.toggle}>
                                <input checked={actorSettings.show_content_warning} onChange={(event) => void saveActorSettings({ show_content_warning: event.target.checked })} style={styles.checkbox} type="checkbox" />
                                <span>CW本文を初期表示する</span>
                            </label>
                        </div>
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
                        <Button
                            onClick={() => {
                                if (window.confirm(`@${selectedActor.username}を削除しますか？`))
                                    void api
                                        .deleteActor(csrf, selectedActor.id)
                                        .then(onActorsChanged)
                                        .catch((reason) => setError(reason instanceof Error ? reason.message : "削除できませんでした"));
                            }}
                            variant="danger"
                        >
                            <IconTrash />
                            このActorを削除
                        </Button>
                    </section>
                )}
            </div>
        </>
    );
}
