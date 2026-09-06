import { type CSSProperties, useCallback, useEffect, useMemo, useState } from "react";

import { IconLeaf2, IconPlus } from "@tabler/icons-react";

import { AppShell } from "./components/AppShell";
import { AuthScreen } from "./components/AuthScreen";
import { Composer, type ComposerIntent } from "./components/Composer";
import { Button, ErrorBanner, Loading } from "./components/ui";
import { ApiError, api, type Page } from "./lib/api";
import { css } from "./lib/css";
import type { AccountSettings, Actor, ActorSettings, Emoji, Session } from "./lib/schema";
import { FollowRequestsPage } from "./pages/FollowRequestsPage";
import { NotePage } from "./pages/NotePage";
import { NotificationsPage } from "./pages/NotificationsPage";
import { ProfilePage } from "./pages/ProfilePage";
import { SettingsPage } from "./pages/SettingsPage";
import { TimelinePage } from "./pages/TimelinePage";
import { UserSearchPage } from "./pages/UserSearchPage";

type AuthState = "loading" | "setup" | "login" | "authenticated";
const defaultSettings: AccountSettings = { theme: "yellow", reduce_motion: false, compact_mode: false };
const actorCanAct = (actor: Actor) => !actor.is_suspended && !actor.moved_to_uri;

const styles = {
    fullPage: { minHeight: "100dvh", padding: "2.5rem 1.25rem", display: "grid", placeItems: "center", background: "radial-gradient(circle at 50% 0, var(--accent-soft), transparent 38%), var(--page)" },
    fatalPage: { maxWidth: "28rem", marginInline: "auto", display: "flex", flexDirection: "column", justifyContent: "center", gap: "1rem", textAlign: "center" },
    splash: { alignContent: "center", gap: "1rem" },
    mark: { width: "2.75rem", height: "2.75rem", display: "grid", placeItems: "center", borderRadius: "1rem", color: "var(--accent-ink)", background: "linear-gradient(135deg, #f8d56a, var(--accent))", boxShadow: "0 8px 22px #e9a91d3d" },
    fatalTitle: { fontSize: "1.5rem", lineHeight: 1.333, fontWeight: 900 },
    noActor: { width: "100%", maxWidth: "28rem", padding: "1.5rem", border: "1px solid var(--border)", borderRadius: "1.5rem", textAlign: "center", background: "var(--panel)", boxShadow: "0 20px 25px -5px #0000001a, 0 8px 10px -6px #0000001a" },
    noActorMark: { marginInline: "auto", marginBottom: "1rem" },
    noActorText: { margin: "0.5rem 0 1.5rem", color: "var(--muted)", fontSize: "0.875rem" },
    field: { display: "block", marginBottom: "1rem", textAlign: "left" },
    fieldLabel: { display: "block", marginBottom: "0.375rem", fontSize: "0.875rem", fontWeight: 700 },
    input: { width: "100%", padding: "0.625rem 1rem", borderWidth: 1, borderStyle: "solid", borderRadius: "1rem", outline: "none", color: "var(--text)", transition: "border-color 150ms, background-color 150ms" },
} satisfies Record<string, CSSProperties>;

const rules = {
    mark: css({ "& svg": { width: "1.5rem", height: "1.5rem" } }),
    input: css({ borderColor: "var(--border)", background: "var(--panel-muted)", "&:focus": { borderColor: "var(--accent-hover)", background: "var(--panel)" } }),
};

const routeFromPath = (path: string): { page: Page; profileID?: string; noteID?: string } => {
    if (path === "/public") return { page: "public" };
    if (path === "/users") return { page: "users" };
    if (path === "/notifications") return { page: "notifications" };
    if (path === "/follow-requests") return { page: "follow-requests" };
    if (path === "/settings") return { page: "settings" };
    if (path.startsWith("/profiles/")) return { page: "profile", profileID: decodeURIComponent(path.slice(10)) };
    if (path.startsWith("/notes/")) return { page: "note", noteID: decodeURIComponent(path.slice(7)) };
    return { page: "home" };
};

function App() {
    const [authState, setAuthState] = useState<AuthState>("loading");
    const [session, setSession] = useState<Session>();
    const [actors, setActors] = useState<Actor[]>([]);
    const [selectedActorID, setSelectedActorID] = useState("");
    const [settings, setSettings] = useState<AccountSettings>(defaultSettings);
    const [emojis, setEmojis] = useState<Emoji[]>([]);
    const [route, setRoute] = useState(() => routeFromPath(window.location.pathname));
    const [composerIntent, setComposerIntent] = useState<ComposerIntent>();
    const [composerSettings, setComposerSettings] = useState<ActorSettings>();
    const [refreshKey, setRefreshKey] = useState(0);
    const [error, setError] = useState("");

    const loadWorkspace = useCallback(async () => {
        const current = await api.session();
        const [ownedActors, accountSettings] = await Promise.all([api.actors(), api.accountSettings()]);
        setSession(current);
        setActors(ownedActors);
        setSettings(accountSettings);
        const availableActors = ownedActors.filter(actorCanAct);
        const preferred = availableActors.find((actor) => actor.id === accountSettings.selected_actor_id);
        setSelectedActorID((existing) => (availableActors.some((actor) => actor.id === existing) ? existing : preferred?.id || availableActors[0]?.id || ""));
        setAuthState("authenticated");
        void api
            .emojis()
            .then(setEmojis)
            .catch(() => setEmojis([]));
    }, []);

    useEffect(() => {
        api.setupStatus()
            .then(async (result) => {
                if (result.data.setup_required) {
                    setAuthState("setup");
                    return;
                }
                try {
                    await loadWorkspace();
                } catch (reason) {
                    if (reason instanceof ApiError && reason.status === 401) setAuthState("login");
                    else setError(reason instanceof Error ? reason.message : "Salviaを起動できませんでした");
                }
            })
            .catch((reason) => setError(reason instanceof Error ? reason.message : "Rosmarinusに接続できませんでした"));
    }, [loadWorkspace]);

    useEffect(() => {
        const onPopState = () => setRoute(routeFromPath(window.location.pathname));
        window.addEventListener("popstate", onPopState);
        return () => window.removeEventListener("popstate", onPopState);
    }, []);

    useEffect(() => {
        const sessionLost = () => {
            setSession(undefined);
            setActors([]);
            setAuthState("login");
        };
        window.addEventListener("salvia:session-lost", sessionLost);
        return () => window.removeEventListener("salvia:session-lost", sessionLost);
    }, []);

    useEffect(() => {
        document.documentElement.dataset.theme = settings.theme;
        document.documentElement.dataset.compact = String(settings.compact_mode);
        document.documentElement.dataset.reduceMotion = String(settings.reduce_motion);
    }, [settings]);

    useEffect(() => {
        if (authState !== "authenticated") return;
        const source = new EventSource(api.eventURL, { withCredentials: true });
        const channel = "BroadcastChannel" in window ? new BroadcastChannel("salvia-projections") : undefined;
        let refreshTimer = 0;
        const refresh = () => {
            window.clearTimeout(refreshTimer);
            refreshTimer = window.setTimeout(() => setRefreshKey((value) => value + 1), 80);
        };
        const refreshForEvent = (event: Event) => {
            try {
                const actorID = JSON.parse((event as MessageEvent<string>).data).actor_id as string | undefined;
                if (!actorID || actorID === selectedActorID) refresh();
                channel?.postMessage(actorID || "*");
            } catch {
                refresh();
            }
        };
        const refreshWorkspace = () => void loadWorkspace().catch((reason) => setError(reason instanceof Error ? reason.message : "Actor一覧を更新できませんでした"));
        const actorEventTypes = ["actor.created", "actor.updated", "actor.deleted"];
        const projectionEventTypes = ["note.created", "note.deleted", "reaction.changed", "notification.created", "notification.read", "follow.approval.requested", "follow.approval.completed", "follow.approval.rejected", "follow.changed", "block.changed", "poll.changed", "projection.invalidated"];
        for (const type of actorEventTypes) source.addEventListener(type, refreshWorkspace);
        for (const type of projectionEventTypes) source.addEventListener(type, refreshForEvent);
        if (channel)
            channel.onmessage = (event: MessageEvent<string>) => {
                if (event.data === "*" || event.data === selectedActorID) refresh();
            };
        const onVisible = () => {
            if (document.visibilityState === "visible") refresh();
        };
        document.addEventListener("visibilitychange", onVisible);
        source.onopen = () => {
            refresh();
            refreshWorkspace();
        };
        return () => {
            for (const type of actorEventTypes) source.removeEventListener(type, refreshWorkspace);
            for (const type of projectionEventTypes) source.removeEventListener(type, refreshForEvent);
            document.removeEventListener("visibilitychange", onVisible);
            window.clearTimeout(refreshTimer);
            channel?.close();
            source.close();
        };
    }, [authState, loadWorkspace, selectedActorID]);

    const selectedActor = useMemo(() => actors.find((actor) => actor.id === selectedActorID), [actors, selectedActorID]);
    const navigate = (path: string) => {
        window.history.pushState({}, "", path);
        setRoute(routeFromPath(path));
        window.scrollTo({ top: 0, behavior: settings.reduce_motion ? "auto" : "smooth" });
    };
    const chooseActor = async (id: string) => {
        if (!actors.some((actor) => actor.id === id && actorCanAct(actor))) {
            setError("停止または移行済みのActorには切り替えられません");
            return;
        }
        setSelectedActorID(id);
        setRefreshKey((value) => value + 1);
        try {
            setSettings(await api.updateAccountSettings(session?.csrf_token || "", { selected_actor_id: id }));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "Actorを切り替えられませんでした");
        }
    };
    const openComposer = async (intent: ComposerIntent = { kind: "post" }) => {
        if (!selectedActor) return;
        setComposerIntent(intent);
        try {
            setComposerSettings(await api.actorSettings(selectedActor.id));
        } catch {
            setComposerSettings(undefined);
        }
    };
    const logout = async () => {
        if (!session) return;
        try {
            await api.logout(session.csrf_token);
        } finally {
            setSession(undefined);
            setActors([]);
            setAuthState("login");
            navigate("/");
        }
    };

    if (error && authState === "loading")
        return (
            <main style={{ ...styles.fullPage, ...styles.fatalPage }}>
                <span className={rules.mark} style={styles.mark}>
                    <IconLeaf2 />
                </span>
                <h1 style={styles.fatalTitle}>Rosmarinusに接続できません</h1>
                <ErrorBanner message={error} />
                <Button onClick={() => window.location.reload()}>再読み込み</Button>
            </main>
        );
    if (authState === "loading")
        return (
            <main style={{ ...styles.fullPage, ...styles.splash }}>
                <span className={rules.mark} style={styles.mark}>
                    <IconLeaf2 />
                </span>
                <Loading label="Salviaを起動中" />
            </main>
        );
    if (authState === "setup" || authState === "login") return <AuthScreen mode={authState} onAuthenticated={loadWorkspace} />;
    if (!session) return null;
    if (!selectedActor) return <NoActor csrf={session.csrf_token} onCreated={loadWorkspace} onLogout={logout} />;

    return (
        <AppShell actors={actors.filter(actorCanAct)} onActorChange={(id) => void chooseActor(id)} onCompose={() => void openComposer()} onLogout={() => void logout()} onNavigate={navigate} page={route.page} selectedActor={selectedActor} session={session}>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {route.page === "home" && (
                <TimelinePage
                    actorID={selectedActor.id}
                    csrf={session.csrf_token}
                    emojis={emojis}
                    kind="home"
                    onCompose={(kind, note) => void openComposer({ kind, target: note })}
                    onOpenNote={(id) => navigate(`/notes/${encodeURIComponent(id)}`)}
                    onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)}
                    refreshKey={refreshKey}
                />
            )}
            {route.page === "public" && (
                <TimelinePage
                    actorID={selectedActor.id}
                    csrf={session.csrf_token}
                    emojis={emojis}
                    kind="public"
                    onCompose={(kind, note) => void openComposer({ kind, target: note })}
                    onOpenNote={(id) => navigate(`/notes/${encodeURIComponent(id)}`)}
                    onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)}
                    refreshKey={refreshKey}
                />
            )}
            {route.page === "users" && <UserSearchPage actorID={selectedActor.id} csrf={session.csrf_token} onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)} />}
            {route.page === "notifications" && <NotificationsPage actorID={selectedActor.id} csrf={session.csrf_token} onActorChange={(id) => void chooseActor(id)} onOpenNote={(id) => navigate(`/notes/${encodeURIComponent(id)}`)} refreshKey={refreshKey} />}
            {route.page === "follow-requests" && <FollowRequestsPage actorID={selectedActor.id} csrf={session.csrf_token} refreshKey={refreshKey} />}
            {route.page === "settings" && <SettingsPage accountSettings={settings} actors={actors} csrf={session.csrf_token} onActorsChanged={loadWorkspace} onSettingsChanged={setSettings} selectedActor={selectedActor} />}
            {route.page === "profile" && route.profileID && <ProfilePage actorID={selectedActor.id} csrf={session.csrf_token} onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)} profileID={route.profileID} />}
            {route.page === "note" && route.noteID && (
                <NotePage
                    actorID={selectedActor.id}
                    csrf={session.csrf_token}
                    emojis={emojis}
                    noteID={route.noteID}
                    onBack={() => window.history.back()}
                    onCompose={(kind, note) => void openComposer({ kind, target: note })}
                    onOpenNote={(id) => navigate(`/notes/${encodeURIComponent(id)}`)}
                    onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)}
                    refreshKey={refreshKey}
                />
            )}
            {composerIntent && (
                <Composer
                    actor={selectedActor}
                    actorSettings={composerSettings}
                    csrf={session.csrf_token}
                    intent={composerIntent}
                    onClose={() => setComposerIntent(undefined)}
                    onSubmit={async (input, intentKey, noteID) => {
                        await api.createPost(session.csrf_token, selectedActor.id, input, intentKey, noteID);
                        setRefreshKey((value) => value + 1);
                    }}
                />
            )}
        </AppShell>
    );
}

function NoActor({ csrf, onCreated, onLogout }: { csrf: string; onCreated: () => Promise<void>; onLogout: () => Promise<void> }) {
    const [username, setUsername] = useState("");
    const [name, setName] = useState("");
    const [error, setError] = useState("");
    return (
        <main style={styles.fullPage}>
            <form
                onSubmit={(event) => {
                    event.preventDefault();
                    api.createActor(csrf, username.trim(), name.trim())
                        .then(onCreated)
                        .catch((reason) => setError(reason instanceof Error ? reason.message : "Actorを作成できませんでした"));
                }}
                style={styles.noActor}
            >
                <span className={rules.mark} style={{ ...styles.mark, ...styles.noActorMark }}>
                    <IconLeaf2 />
                </span>
                <h1 style={styles.fatalTitle}>最初のActorを作成</h1>
                <p style={styles.noActorText}>投稿やフォローに使う公開アイデンティティです。</p>
                {error && <ErrorBanner message={error} />}
                <label style={styles.field}>
                    <span style={styles.fieldLabel}>ユーザー名</span>
                    <input className={rules.input} maxLength={64} onChange={(event) => setUsername(event.target.value)} required style={styles.input} value={username} />
                </label>
                <label style={styles.field}>
                    <span style={styles.fieldLabel}>表示名</span>
                    <input className={rules.input} maxLength={128} onChange={(event) => setName(event.target.value)} style={styles.input} value={name} />
                </label>
                <Button style={{ width: "100%" }} type="submit">
                    <IconPlus />
                    Actorを作成
                </Button>
                <Button onClick={() => void onLogout()} variant="ghost">
                    ログアウト
                </Button>
            </form>
        </main>
    );
}

export default App;
