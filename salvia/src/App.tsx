import { useCallback, useEffect, useMemo, useState } from "react";

import { IconLeaf2, IconPlus } from "@tabler/icons-react";

import "./App.css";
import { AppShell } from "./components/AppShell";
import { AuthScreen } from "./components/AuthScreen";
import { Composer } from "./components/Composer";
import { Button, ErrorBanner, Loading } from "./components/ui";
import { ApiError, api, type Page } from "./lib/api";
import type { AccountSettings, Actor, ActorSettings, Session } from "./lib/schema";
import { FollowRequestsPage } from "./pages/FollowRequestsPage";
import { NotificationsPage } from "./pages/NotificationsPage";
import { ProfilePage } from "./pages/ProfilePage";
import { SettingsPage } from "./pages/SettingsPage";
import { TimelinePage } from "./pages/TimelinePage";

type AuthState = "loading" | "setup" | "login" | "authenticated";
const defaultSettings: AccountSettings = { theme: "yellow", reduce_motion: false, compact_mode: false };

const routeFromPath = (path: string): { page: Page; profileID?: string } => {
    if (path === "/public") return { page: "public" };
    if (path === "/notifications") return { page: "notifications" };
    if (path === "/follow-requests") return { page: "follow-requests" };
    if (path === "/settings") return { page: "settings" };
    if (path.startsWith("/profiles/")) return { page: "profile", profileID: decodeURIComponent(path.slice(10)) };
    return { page: "home" };
};

function App() {
    const [authState, setAuthState] = useState<AuthState>("loading");
    const [session, setSession] = useState<Session>();
    const [actors, setActors] = useState<Actor[]>([]);
    const [selectedActorID, setSelectedActorID] = useState("");
    const [settings, setSettings] = useState<AccountSettings>(defaultSettings);
    const [route, setRoute] = useState(() => routeFromPath(window.location.pathname));
    const [composerOpen, setComposerOpen] = useState(false);
    const [composerSettings, setComposerSettings] = useState<ActorSettings>();
    const [refreshKey, setRefreshKey] = useState(0);
    const [error, setError] = useState("");

    const loadWorkspace = useCallback(async () => {
        const current = await api.session();
        const [ownedActors, accountSettings] = await Promise.all([api.actors(), api.accountSettings()]);
        setSession(current);
        setActors(ownedActors);
        setSettings(accountSettings);
        const preferred = ownedActors.find((actor) => actor.id === accountSettings.selected_actor_id);
        setSelectedActorID((existing) => (ownedActors.some((actor) => actor.id === existing) ? existing : preferred?.id || ownedActors[0]?.id || ""));
        setAuthState("authenticated");
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
        document.documentElement.dataset.theme = settings.theme;
        document.documentElement.dataset.compact = String(settings.compact_mode);
        document.documentElement.dataset.reduceMotion = String(settings.reduce_motion);
    }, [settings]);

    useEffect(() => {
        if (authState !== "authenticated") return;
        const source = new EventSource(api.eventURL, { withCredentials: true });
        const refresh = () => setRefreshKey((value) => value + 1);
        const refreshWorkspace = () => void loadWorkspace().catch((reason) => setError(reason instanceof Error ? reason.message : "Actor一覧を更新できませんでした"));
        const actorEventTypes = ["actor.created", "actor.updated", "actor.deleted"];
        const projectionEventTypes = ["note.created", "note.deleted", "reaction.changed", "notification.created", "notification.read", "follow.approval.requested", "follow.approval.completed", "follow.approval.rejected", "follow.changed", "block.changed", "poll.changed", "projection.invalidated"];
        for (const type of actorEventTypes) source.addEventListener(type, refreshWorkspace);
        for (const type of projectionEventTypes) source.addEventListener(type, refresh);
        source.onopen = () => {
            refresh();
            refreshWorkspace();
        };
        return () => {
            for (const type of actorEventTypes) source.removeEventListener(type, refreshWorkspace);
            for (const type of projectionEventTypes) source.removeEventListener(type, refresh);
            source.close();
        };
    }, [authState, loadWorkspace]);

    const selectedActor = useMemo(() => actors.find((actor) => actor.id === selectedActorID), [actors, selectedActorID]);
    const navigate = (path: string) => {
        window.history.pushState({}, "", path);
        setRoute(routeFromPath(path));
        window.scrollTo({ top: 0, behavior: settings.reduce_motion ? "auto" : "smooth" });
    };
    const chooseActor = async (id: string) => {
        setSelectedActorID(id);
        setRefreshKey((value) => value + 1);
        try {
            setSettings(await api.updateAccountSettings(session?.csrf_token || "", { selected_actor_id: id }));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "Actorを切り替えられませんでした");
        }
    };
    const openComposer = async () => {
        if (!selectedActor) return;
        setComposerOpen(true);
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
            <main className="fatal-page">
                <span className="brand-mark">
                    <IconLeaf2 />
                </span>
                <h1>Rosmarinusに接続できません</h1>
                <ErrorBanner message={error} />
                <Button onClick={() => window.location.reload()}>再読み込み</Button>
            </main>
        );
    if (authState === "loading")
        return (
            <main className="splash">
                <span className="brand-mark">
                    <IconLeaf2 />
                </span>
                <Loading label="Salviaを起動中" />
            </main>
        );
    if (authState === "setup" || authState === "login") return <AuthScreen mode={authState} onAuthenticated={loadWorkspace} />;
    if (!session) return null;
    if (!selectedActor) return <NoActor csrf={session.csrf_token} onCreated={loadWorkspace} onLogout={logout} />;

    return (
        <AppShell actors={actors} onActorChange={(id) => void chooseActor(id)} onCompose={() => void openComposer()} onLogout={() => void logout()} onNavigate={navigate} page={route.page} selectedActor={selectedActor} session={session}>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {route.page === "home" && <TimelinePage actorID={selectedActor.id} csrf={session.csrf_token} kind="home" onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)} refreshKey={refreshKey} />}
            {route.page === "public" && <TimelinePage actorID={selectedActor.id} csrf={session.csrf_token} kind="public" onOpenProfile={(id) => navigate(`/profiles/${encodeURIComponent(id)}`)} refreshKey={refreshKey} />}
            {route.page === "notifications" && <NotificationsPage actorID={selectedActor.id} csrf={session.csrf_token} refreshKey={refreshKey} />}
            {route.page === "follow-requests" && <FollowRequestsPage actorID={selectedActor.id} csrf={session.csrf_token} refreshKey={refreshKey} />}
            {route.page === "settings" && <SettingsPage accountSettings={settings} actors={actors} csrf={session.csrf_token} onActorsChanged={loadWorkspace} onSettingsChanged={setSettings} selectedActor={selectedActor} />}
            {route.page === "profile" && route.profileID && <ProfilePage actorID={selectedActor.id} csrf={session.csrf_token} profileID={route.profileID} />}
            {composerOpen && (
                <Composer
                    actor={selectedActor}
                    actorSettings={composerSettings}
                    onClose={() => setComposerOpen(false)}
                    onSubmit={async (text, visibility, contentWarning) => {
                        await api.createPost(session.csrf_token, selectedActor.id, text, visibility, contentWarning);
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
        <main className="auth-page">
            <form
                className="auth-card no-actor"
                onSubmit={(event) => {
                    event.preventDefault();
                    api.createActor(csrf, username.trim(), name.trim())
                        .then(onCreated)
                        .catch((reason) => setError(reason instanceof Error ? reason.message : "Actorを作成できませんでした"));
                }}
            >
                <span className="brand-mark">
                    <IconLeaf2 />
                </span>
                <h1>最初のActorを作成</h1>
                <p>投稿やフォローに使う公開アイデンティティです。</p>
                {error && <ErrorBanner message={error} />}
                <label className="field">
                    <span>ユーザー名</span>
                    <input maxLength={64} onChange={(event) => setUsername(event.target.value)} required value={username} />
                </label>
                <label className="field">
                    <span>表示名</span>
                    <input maxLength={128} onChange={(event) => setName(event.target.value)} value={name} />
                </label>
                <Button className="button--wide" type="submit">
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
