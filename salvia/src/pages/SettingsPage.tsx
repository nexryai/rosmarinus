import { type FormEvent, useEffect, useState } from "react";

import { IconPalette, IconPlus, IconTrash, IconUserCircle } from "@tabler/icons-react";

import { Button, ErrorBanner } from "../components/ui";
import { api } from "../lib/api";
import type { AccountSettings, Actor, ActorSettings } from "../lib/schema";

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
            <header className="page-header">
                <div>
                    <p className="eyebrow">アカウントとActor</p>
                    <h1>設定</h1>
                </div>
                <IconPalette className="header-icon" />
            </header>
            <div className="settings-stack">
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                {message && (
                    <p className="success-banner" role="status">
                        {message}
                    </p>
                )}
                <section className="settings-card">
                    <h2>表示</h2>
                    <label className="field">
                        <span>テーマ</span>
                        <select onChange={(event) => void updateAccount({ theme: event.target.value as AccountSettings["theme"] })} value={accountSettings.theme}>
                            <option value="yellow">Salvia Yellow</option>
                            <option value="light">ライト</option>
                            <option value="dark">ダーク</option>
                            <option value="system">システム</option>
                        </select>
                    </label>
                    <label className="toggle">
                        <input checked={accountSettings.reduce_motion} onChange={(event) => void updateAccount({ reduce_motion: event.target.checked })} type="checkbox" />
                        <span>動きを減らす</span>
                    </label>
                    <label className="toggle">
                        <input checked={accountSettings.compact_mode} onChange={(event) => void updateAccount({ compact_mode: event.target.checked })} type="checkbox" />
                        <span>コンパクト表示</span>
                    </label>
                </section>
                <section className="settings-card">
                    <div className="settings-title">
                        <IconUserCircle />
                        <div>
                            <h2>@{selectedActor.username}</h2>
                            <p>公開プロフィール</p>
                        </div>
                    </div>
                    <form className="form-grid" onSubmit={saveProfile}>
                        <label className="field">
                            <span>表示名</span>
                            <input maxLength={128} onChange={(event) => setName(event.target.value)} value={name} />
                        </label>
                        <label className="field">
                            <span>アバターURL</span>
                            <input inputMode="url" onChange={(event) => setAvatarURL(event.target.value)} placeholder="https://…" value={avatarURL} />
                        </label>
                        <label className="field field--full">
                            <span>自己紹介</span>
                            <textarea maxLength={1500} onChange={(event) => setSummary(event.target.value)} rows={4} value={summary} />
                        </label>
                        <Button type="submit">プロフィールを保存</Button>
                    </form>
                    {actorSettings && (
                        <div className="actor-preferences">
                            <label className="field">
                                <span>標準の公開範囲</span>
                                <select onChange={(event) => void saveActorSettings({ default_visibility: event.target.value as ActorSettings["default_visibility"] })} value={actorSettings.default_visibility}>
                                    <option value="public">公開</option>
                                    <option value="home">ホーム</option>
                                    <option value="followers">フォロワー</option>
                                </select>
                            </label>
                            <label className="toggle">
                                <input checked={actorSettings.pinned} onChange={(event) => void saveActorSettings({ pinned: event.target.checked })} type="checkbox" />
                                <span>Actor切替で上に固定</span>
                            </label>
                            <label className="field">
                                <span>表示色</span>
                                <input onBlur={(event) => void saveActorSettings({ color: event.target.value })} type="color" value={actorSettings.color || "#e9a91d"} onChange={(event) => setActorSettings((current) => (current ? { ...current, color: event.target.value } : current))} />
                            </label>
                            <label className="field">
                                <span>表示順</span>
                                <input
                                    min={0}
                                    onBlur={(event) => void saveActorSettings({ display_order: Number(event.target.value) })}
                                    type="number"
                                    value={actorSettings.display_order}
                                    onChange={(event) => setActorSettings((current) => (current ? { ...current, display_order: Number(event.target.value) } : current))}
                                />
                            </label>
                            <label className="toggle">
                                <input checked={actorSettings.show_content_warning} onChange={(event) => void saveActorSettings({ show_content_warning: event.target.checked })} type="checkbox" />
                                <span>CW本文を初期表示する</span>
                            </label>
                        </div>
                    )}
                </section>
                <section className="settings-card">
                    <h2>Actorを追加</h2>
                    <p>ひとつのアカウントで複数の公開アイデンティティを管理できます。</p>
                    <form className="inline-form" onSubmit={createActor}>
                        <input aria-label="新しいActorのユーザー名" maxLength={64} onChange={(event) => setNewUsername(event.target.value)} placeholder="username" required value={newUsername} />
                        <input aria-label="新しいActorの表示名" maxLength={128} onChange={(event) => setNewName(event.target.value)} placeholder="表示名" value={newName} />
                        <Button type="submit">
                            <IconPlus />
                            追加
                        </Button>
                    </form>
                </section>
                {actors.length > 1 && (
                    <section className="settings-card settings-card--danger">
                        <h2>Actorを削除</h2>
                        <p>@{selectedActor.username}を削除すると元に戻せません。</p>
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
