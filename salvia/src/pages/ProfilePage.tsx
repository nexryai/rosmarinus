import { type CSSProperties, useEffect, useState } from "react";

import { IconBan, IconLink, IconMapPin, IconUserPlus, IconUserX } from "@tabler/icons-react";

import { Avatar, Button, Empty, ErrorBanner, Loading, Modal } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { Connection, Profile } from "../lib/schema";

const styles = {
    hero: { overflow: "hidden", borderBottom: "1px solid var(--border)" },
    banner: { width: "100%", objectFit: "cover", background: "var(--panel-muted)" },
    body: { position: "relative", paddingTop: "3.5rem", paddingBottom: "1.75rem" },
    avatar: { position: "absolute" },
    actions: { position: "absolute", top: "1rem", display: "flex", gap: "0.5rem" },
    title: { marginTop: "0.25rem", fontSize: "1.5rem", lineHeight: 1.333, fontWeight: 900 },
    handle: { color: "var(--muted)", fontSize: "0.875rem" },
    alert: { marginTop: "0.75rem", padding: "0.5rem 0.75rem", borderWidth: 1, borderStyle: "solid", borderRadius: "0.75rem", color: "var(--danger)", fontSize: "0.875rem" },
    summary: { marginTop: "1.25rem", lineHeight: "1.75rem", whiteSpace: "pre-wrap" },
    meta: { marginTop: "1rem", display: "flex", flexWrap: "wrap", gap: "1rem", color: "var(--muted)", fontSize: "0.875rem" },
    fields: { marginTop: "1rem", display: "grid", gap: 1, overflow: "hidden", border: "1px solid var(--border)", borderRadius: "1rem", background: "var(--border)" },
    field: { padding: "0.5rem 0.75rem", display: "grid", gridTemplateColumns: "minmax(6rem, .35fr) 1fr", gap: "0.75rem", background: "var(--panel)", fontSize: "0.875rem" },
    fieldName: { color: "var(--muted)", fontWeight: 700 },
    fieldValue: { overflowWrap: "break-word" },
    tags: { marginTop: "0.75rem", display: "flex", flexWrap: "wrap", gap: "0.5rem", color: "var(--accent-hover)", fontSize: "0.875rem" },
    counts: { marginTop: "1.25rem", display: "flex", gap: "1.5rem", color: "var(--muted)", fontSize: "0.875rem" },
    countButton: { color: "inherit" },
    count: { marginRight: "0.25rem", color: "var(--text)" },
    connectionButton: { width: "100%", padding: "0.75rem", display: "flex", alignItems: "center", gap: "0.75rem", borderRadius: "1rem", textAlign: "left" },
    connectionText: { display: "block" },
    connectionHandle: { display: "block", color: "var(--muted)" },
} satisfies Record<string, CSSProperties>;

const rules = {
    banner: css({ height: "10rem", "@media (width >= 40rem)": { height: "13rem" } }),
    body: css({ paddingInline: "1.5rem", "@media (width >= 40rem)": { paddingInline: "2rem" } }),
    avatar: css({ left: "1.5rem", "@media (width >= 40rem)": { left: "2rem" } }),
    actions: css({ right: "1.5rem", "@media (width >= 40rem)": { right: "2rem" } }),
    meta: css({ "& span, & a": { display: "inline-flex", alignItems: "center", gap: "0.25rem" }, "& svg": { width: "1rem", height: "1rem" } }),
    countButton: css({ "&:hover": { textDecoration: "underline" } }),
    connectionButton: css({ "&:hover": { background: "var(--panel-muted)" } }),
    alert: css({ borderColor: "var(--danger)", background: "var(--danger)", "@supports (color: color-mix(in lab, red, red))": { borderColor: "color-mix(in srgb, var(--danger) 20%, var(--border))", background: "color-mix(in srgb, var(--danger) 6%, var(--panel))" } }),
};

const actorHandle = (profile: Profile) => {
    try {
        return `@${profile.actor.username}@${new URL(profile.actor.uri).host}`;
    } catch {
        return `@${profile.actor.username}`;
    }
};

export function ProfilePage({ actorID, csrf, onOpenProfile, profileID }: { actorID: string; csrf: string; onOpenProfile: (actorID: string) => void; profileID: string }) {
    const [profile, setProfile] = useState<Profile>();
    const [following, setFollowing] = useState(false);
    const [blocked, setBlocked] = useState(false);
    const [connections, setConnections] = useState<{ kind: "followers" | "following"; items: Connection[] }>();
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    useEffect(() => {
        const controller = new AbortController();
        setLoading(true);
        api.profile(actorID, profileID, controller.signal)
            .then((value) => {
                setProfile(value);
                setFollowing(value.follow_status === "accepted" || value.follow_status === "pending");
                setBlocked(value.blocked_by_viewer);
            })
            .catch((reason) => {
                if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "プロフィールを読み込めませんでした");
            })
            .finally(() => {
                if (!controller.signal.aborted) setLoading(false);
            });
        return () => controller.abort();
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
            <header style={styles.hero}>
                {actor.banner_url && <img alt="" className={rules.banner} referrerPolicy="no-referrer" src={actor.banner_url} style={styles.banner} />}
                <div className={rules.body} style={styles.body}>
                    <div className={rules.avatar} style={{ ...styles.avatar, top: actor.banner_url ? "-3rem" : "1rem" }}>
                        <Avatar actor={actor} size="large" />
                    </div>
                    <div className={rules.actions} style={styles.actions}>
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
                    <h1 style={styles.title}>{actor.name || actor.username}</h1>
                    <p style={styles.handle}>{actorHandle(profile)}</p>
                    {actor.is_suspended && (
                        <p className={rules.alert} style={styles.alert}>
                            このActorは停止されています。
                        </p>
                    )}
                    {actor.moved_to_uri && (
                        <p className={rules.alert} style={styles.alert}>
                            このActorは{" "}
                            <a href={actor.moved_to_uri} rel="noreferrer" target="_blank">
                                別のActorへ移行しました
                            </a>
                            。
                        </p>
                    )}
                    {actor.summary && <p style={styles.summary}>{actor.summary}</p>}
                    <div className={rules.meta} style={styles.meta}>
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
                    {actor.profile_fields.length > 0 && (
                        <dl style={styles.fields}>
                            {actor.profile_fields.map((field) => (
                                <div key={field.name} style={styles.field}>
                                    <dt style={styles.fieldName}>{field.name}</dt>
                                    <dd style={styles.fieldValue}>{field.value}</dd>
                                </div>
                            ))}
                        </dl>
                    )}
                    {actor.tags.length > 0 && (
                        <div style={styles.tags}>
                            {actor.tags.map((tag) => (
                                <span key={tag}>#{tag}</span>
                            ))}
                        </div>
                    )}
                    <div style={styles.counts}>
                        <button className={rules.countButton} disabled={busy} onClick={() => void showConnections("following")} style={styles.countButton} type="button">
                            <strong style={styles.count}>{profile.following_count}</strong>フォロー
                        </button>
                        <button className={rules.countButton} disabled={busy} onClick={() => void showConnections("followers")} style={styles.countButton} type="button">
                            <strong style={styles.count}>{profile.followers_count}</strong>フォロワー
                        </button>
                    </div>
                </div>
            </header>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {connections && (
                <Modal label={connections.kind === "followers" ? "フォロワー" : "フォロー中"} onClose={() => setConnections(undefined)}>
                    <section>
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
                                    className={rules.connectionButton}
                                    style={styles.connectionButton}
                                    type="button"
                                >
                                    <Avatar actor={item.actor} />
                                    <span style={styles.connectionText}>
                                        <strong>{item.actor.name || item.actor.username}</strong>
                                        <small style={styles.connectionHandle}>@{item.actor.username}</small>
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
