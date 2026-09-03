import { useEffect, useState } from "react";

import { IconBan, IconLink, IconMapPin, IconUserPlus, IconUserX } from "@tabler/icons-react";

import { Avatar, Button, Empty, ErrorBanner, Loading, Modal } from "../components/ui";
import { api } from "../lib/api";
import type { Connection, Profile } from "../lib/schema";

export function ProfilePage({ actorID, csrf, onOpenProfile, profileID }: { actorID: string; csrf: string; onOpenProfile: (actorID: string) => void; profileID: string }) {
    const [profile, setProfile] = useState<Profile>();
    const [following, setFollowing] = useState(false);
    const [blocked, setBlocked] = useState(false);
    const [connections, setConnections] = useState<{ kind: "followers" | "following"; items: Connection[] }>();
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    useEffect(() => {
        setLoading(true);
        Promise.all([api.profile(actorID, profileID), api.following(actorID)])
            .then(([value, items]) => {
                setProfile(value);
                setFollowing(items.some((item) => item.actor.id === profileID));
                setBlocked(false);
            })
            .catch((reason) => setError(reason instanceof Error ? reason.message : "プロフィールを読み込めませんでした"))
            .finally(() => setLoading(false));
    }, [actorID, profileID]);
    const toggleFollow = async () => {
        if (!profile) return;
        setBusy(true);
        setError("");
        try {
            if (following) await api.unfollow(csrf, actorID, profile.actor.uri);
            else await api.follow(csrf, actorID, profile.actor.uri);
            setFollowing((value) => !value);
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "フォロー状態を変更できませんでした");
        } finally {
            setBusy(false);
        }
    };
    const toggleBlock = async () => {
        if (!profile) return;
        if (!blocked && !window.confirm(`${profile.actor.name || profile.actor.username}をブロックしますか？`)) return;
        setBusy(true);
        setError("");
        try {
            if (blocked) await api.unblock(csrf, actorID, profile.actor.uri);
            else await api.block(csrf, actorID, profile.actor.uri);
            setBlocked((value) => !value);
            setFollowing(false);
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "ブロック状態を変更できませんでした");
        } finally {
            setBusy(false);
        }
    };
    const showConnections = async (kind: "followers" | "following") => {
        setBusy(true);
        setError("");
        try {
            setConnections({ kind, items: await api.profileConnections(actorID, profileID, kind) });
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "つながりを読み込めませんでした");
        } finally {
            setBusy(false);
        }
    };
    if (loading) return <Loading label="プロフィールを読み込み中" />;
    if (!profile) return <ErrorBanner message={error || "プロフィールが見つかりません"} />;
    const actor = profile.actor;
    return (
        <>
            <header className="profile-hero">
                {actor.banner_url && <img alt="" className="profile-hero__banner" referrerPolicy="no-referrer" src={actor.banner_url} />}
                <div className="profile-hero__body">
                    <Avatar actor={actor} size="large" />
                    <div className="profile-hero__actions">
                        {actor.id !== actorID && (
                            <>
                                <Button disabled={busy || blocked} onClick={() => void toggleFollow()} variant={following ? "secondary" : "primary"}>
                                    {following ? (
                                        <>
                                            <IconUserX />
                                            フォロー解除
                                        </>
                                    ) : (
                                        <>
                                            <IconUserPlus />
                                            フォロー
                                        </>
                                    )}
                                </Button>
                                <Button disabled={busy} onClick={() => void toggleBlock()} variant="ghost">
                                    <IconBan />
                                    {blocked ? "ブロック解除" : "ブロック"}
                                </Button>
                            </>
                        )}
                    </div>
                    <h1>{actor.name || actor.username}</h1>
                    <p className="handle">@{actor.username}</p>
                    {actor.is_suspended && <p className="profile-alert">このActorは停止されています。</p>}
                    {actor.moved_to_uri && (
                        <p className="profile-alert">
                            このActorは{" "}
                            <a href={actor.moved_to_uri} rel="noreferrer" target="_blank">
                                別のActorへ移行しました
                            </a>
                            。
                        </p>
                    )}
                    {actor.summary && <p className="profile-summary">{actor.summary}</p>}
                    <div className="profile-meta">
                        {actor.location && (
                            <span>
                                <IconMapPin />
                                {actor.location}
                            </span>
                        )}
                        {actor.url && (
                            <a href={actor.url} rel="noreferrer" target="_blank">
                                <IconLink />
                                リンク
                            </a>
                        )}
                    </div>
                    <div className="profile-counts">
                        <button disabled={busy} onClick={() => void showConnections("following")} type="button">
                            <strong>{profile.following_count}</strong>フォロー
                        </button>
                        <button disabled={busy} onClick={() => void showConnections("followers")} type="button">
                            <strong>{profile.followers_count}</strong>フォロワー
                        </button>
                    </div>
                </div>
            </header>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {connections && (
                <Modal label={connections.kind === "followers" ? "フォロワー" : "フォロー中"} onClose={() => setConnections(undefined)}>
                    <section className="connection-list">
                        {connections.items.length === 0 ? (
                            <Empty>まだ誰もいません。</Empty>
                        ) : (
                            connections.items.map((item) => (
                                <button
                                    key={item.id}
                                    onClick={() => {
                                        setConnections(undefined);
                                        onOpenProfile(item.actor.id);
                                    }}
                                    type="button"
                                >
                                    <Avatar actor={item.actor} />
                                    <span>
                                        <strong>{item.actor.name || item.actor.username}</strong>
                                        <small>@{item.actor.username}</small>
                                    </span>
                                </button>
                            ))
                        )}
                    </section>
                </Modal>
            )}
        </>
    );
}
