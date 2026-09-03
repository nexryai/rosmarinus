import { useEffect, useState } from "react";

import { IconLink, IconMapPin, IconUserPlus, IconUserX } from "@tabler/icons-react";

import { Avatar, Button, ErrorBanner, Loading } from "../components/ui";
import { api } from "../lib/api";
import type { Profile } from "../lib/schema";

export function ProfilePage({ actorID, csrf, profileID }: { actorID: string; csrf: string; profileID: string }) {
    const [profile, setProfile] = useState<Profile>();
    const [following, setFollowing] = useState(false);
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    useEffect(() => {
        setLoading(true);
        Promise.all([api.profile(actorID, profileID), api.following(actorID)])
            .then(([value, connections]) => {
                setProfile(value);
                setFollowing(connections.some((connection) => connection.actor.id === profileID));
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
                            <Button disabled={busy} onClick={() => void toggleFollow()} variant={following ? "secondary" : "primary"}>
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
                        )}
                    </div>
                    <h1>{actor.name || actor.username}</h1>
                    <p className="handle">@{actor.username}</p>
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
                        <span>
                            <strong>{profile.following_count}</strong>フォロー
                        </span>
                        <span>
                            <strong>{profile.followers_count}</strong>フォロワー
                        </span>
                    </div>
                </div>
            </header>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
        </>
    );
}
