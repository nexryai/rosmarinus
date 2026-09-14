import { type CSSProperties, useCallback, useEffect, useRef, useState } from "react";

import { IconRefresh } from "@tabler/icons-react";

import { NoteCard } from "../components/NoteCard";
import { NoteList, NoteListItem } from "../components/NoteList";
import { Button, Empty, ErrorBanner, Loading, PageHeader, RoundButton } from "../components/ui";
import { api } from "../lib/api";
import { css, keyframes } from "../lib/css";
import type { Emoji, Note } from "../lib/schema";

const noteArrival = keyframes({
    from: {
        opacity: 0,
        transform: "translateY(max(-4rem, -100%))",
        gridTemplateRows: "0fr",
    },
    to: {
        opacity: 1,
        transform: "translateY(0)",
        gridTemplateRows: "1fr",
    },
});

const styles = {
    loadMore: {
        padding: "1.5rem",
        display: "flex",
        justifyContent: "center",
    },
    end: {
        padding: "1.5rem",
        color: "var(--muted)",
        fontSize: "0.8125rem",
        textAlign: "center",
    },
    noteSlot: {
        display: "grid",
        gridTemplateRows: "1fr",
    },
    noteSlotContent: {
        minHeight: 0,
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    arriving: css({
        animation: `${noteArrival} 480ms cubic-bezier(.23,1,.32,1) both`,
        "& > div": {
            overflow: "hidden",
        },
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
        },
    }),
};

export function TimelinePage({
    actorID,
    csrf,
    emojis,
    kind,
    liveRefreshKey,
    onCompose,
    onOpenNote,
    onOpenProfile,
    refreshKey,
}: {
    actorID: string;
    csrf: string;
    emojis: Emoji[];
    kind: "home" | "public";
    liveRefreshKey: number;
    onCompose: (kind: "reply" | "quote", note: Note) => void;
    onOpenNote: (noteID: string) => void;
    onOpenProfile: (actorID: string) => void;
    refreshKey: number;
}) {
    const [notes, setNotes] = useState<Note[]>([]);
    const notesRef = useRef<Note[]>([]);
    const [next, setNext] = useState("");
    const [loading, setLoading] = useState(true);
    const [loadingMore, setLoadingMore] = useState(false);
    const [paginationBlocked, setPaginationBlocked] = useState(false);
    const [error, setError] = useState("");
    const [arrivingIDs, setArrivingIDs] = useState<Set<string>>(() => new Set());
    const loadedContext = useRef("");
    const hasLoaded = useRef(false);
    const previousLiveRefreshKey = useRef(liveRefreshKey);
    const pageCursorInFlight = useRef("");
    const resetGeneration = useRef(0);
    const sentinel = useRef<HTMLDivElement>(null);
    const load = useCallback(
        async (after = "", append = false, animateNew = false, signal?: AbortSignal) => {
            if (append && (!after || pageCursorInFlight.current)) return;
            const generation = append ? resetGeneration.current : resetGeneration.current + 1;
            if (append) {
                pageCursorInFlight.current = after;
                setLoadingMore(true);
            } else {
                resetGeneration.current = generation;
                pageCursorInFlight.current = "";
                setLoadingMore(false);
                setPaginationBlocked(false);
                setLoading(true);
            }
            setError("");
            try {
                const page = await api.timeline(kind, actorID, after, signal);
                if (generation !== resetGeneration.current) return;
                const current = notesRef.current;
                const unseen = page.data.filter((note) => !current.some((item) => item.id === note.id));
                const updated = append ? [...current, ...unseen] : animateNew ? [...unseen, ...current] : page.data;
                setArrivingIDs(animateNew ? new Set(page.data.filter((note) => !current.some((item) => item.id === note.id)).map((note) => note.id)) : new Set());
                notesRef.current = updated;
                setNotes(updated);
                if (!animateNew) setNext(append && page.next === after ? "" : page.next);
                setPaginationBlocked(false);
                loadedContext.current = `${kind}:${actorID}`;
                hasLoaded.current = true;
            } catch (reason) {
                if (!signal?.aborted && generation === resetGeneration.current) {
                    setError(reason instanceof Error ? reason.message : "タイムラインを読み込めませんでした");
                    if (append) setPaginationBlocked(true);
                }
            } finally {
                if (append) {
                    if (pageCursorInFlight.current === after) {
                        pageCursorInFlight.current = "";
                        if (!signal?.aborted) setLoadingMore(false);
                    }
                } else if (!signal?.aborted && generation === resetGeneration.current) {
                    setLoading(false);
                }
            }
        },
        [actorID, kind],
    );
    useEffect(() => {
        void refreshKey;
        const context = `${kind}:${actorID}`;
        const sameContext = loadedContext.current === context;
        const animateNew = hasLoaded.current && sameContext && previousLiveRefreshKey.current !== liveRefreshKey;
        if (!sameContext) hasLoaded.current = false;
        previousLiveRefreshKey.current = liveRefreshKey;
        const controller = new AbortController();
        void load("", false, animateNew, controller.signal);
        return () => controller.abort();
    }, [actorID, kind, liveRefreshKey, load, refreshKey]);
    useEffect(() => {
        if (arrivingIDs.size === 0) return;
        const timer = window.setTimeout(() => setArrivingIDs(new Set()), 600);
        return () => window.clearTimeout(timer);
    }, [arrivingIDs]);
    useEffect(() => {
        const target = sentinel.current;
        if (!target || !next || loading || loadingMore || paginationBlocked || typeof IntersectionObserver === "undefined") return;
        const observer = new IntersectionObserver(
            (entries) => {
                if (entries.some((entry) => entry.isIntersecting)) void load(next, true, false);
            },
            { rootMargin: "800px 0px" },
        );
        observer.observe(target);
        return () => observer.disconnect();
    }, [load, loading, loadingMore, next, paginationBlocked]);
    const refresh = () => load();
    const mutate = async (operation: () => Promise<void>) => {
        try {
            await operation();
            await refresh();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "操作に失敗しました");
        }
    };
    return (
        <>
            <PageHeader
                eyebrow={kind === "home" ? "あなたのつながり" : "ローカルと連合"}
                title={kind === "home" ? "ホーム" : "みつける"}
                trailing={
                    <RoundButton aria-label="更新" disabled={loading} onClick={() => void refresh()}>
                        <IconRefresh />
                    </RoundButton>
                }
            />
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading && notes.length === 0 ? (
                <Loading label="タイムラインを読み込み中" />
            ) : notes.length === 0 ? (
                <Empty>まだ表示できるノートがありません。</Empty>
            ) : (
                <NoteList aria-label="ノート一覧">
                    {notes.map((note) => {
                        const arriving = arrivingIDs.has(note.id);
                        return (
                            <NoteListItem
                                className={arriving ? rules.arriving : undefined}
                                data-live-entry={arriving || undefined}
                                key={note.id}
                                onAnimationEnd={(event) => {
                                    if (event.animationName !== noteArrival) return;
                                    setArrivingIDs((current) => {
                                        const nextIDs = new Set(current);
                                        nextIDs.delete(note.id);
                                        return nextIDs;
                                    });
                                }}
                                style={styles.noteSlot}
                            >
                                <div style={styles.noteSlotContent}>
                                    <NoteCard
                                        emojis={emojis}
                                        note={note}
                                        onDelete={(noteID) => mutate(() => api.deletePost(csrf, actorID, noteID))}
                                        onOpenNote={onOpenNote}
                                        onOpenProfile={onOpenProfile}
                                        onQuote={(target) => onCompose("quote", target)}
                                        onReact={(noteID, reaction, reacted) => mutate(() => (reacted ? api.unreact(csrf, actorID, noteID) : api.react(csrf, actorID, noteID, reaction)))}
                                        onRenote={(target) => mutate(() => api.createPost(csrf, actorID, { renote_id: target.id, visibility: target.visibility }))}
                                        onReply={(target) => onCompose("reply", target)}
                                        onVote={(noteID, choice) => mutate(() => api.vote(csrf, actorID, noteID, choice))}
                                        ownActorID={actorID}
                                    />
                                </div>
                            </NoteListItem>
                        );
                    })}
                    {next ? (
                        <div ref={sentinel} style={styles.loadMore}>
                            {loadingMore ? (
                                <Loading label="続きを読み込み中" />
                            ) : (
                                (paginationBlocked || typeof IntersectionObserver === "undefined") && (
                                    <Button onClick={() => void load(next, true, false)} variant="secondary">
                                        {paginationBlocked ? "再試行" : "もっと見る"}
                                    </Button>
                                )
                            )}
                        </div>
                    ) : (
                        <p style={styles.end}>ここまで読みました</p>
                    )}
                </NoteList>
            )}
        </>
    );
}
