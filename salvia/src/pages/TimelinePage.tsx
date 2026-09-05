import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconRefresh } from "@tabler/icons-react";

import { NoteCard } from "../components/NoteCard";
import { Button, DividedList, Empty, ErrorBanner, Loading, PageHeader, RoundButton } from "../components/ui";
import { api } from "../lib/api";
import type { Emoji, Note } from "../lib/schema";

const styles = {
    loadMore: { padding: "1.5rem", display: "flex", justifyContent: "center" },
} satisfies Record<string, CSSProperties>;

export function TimelinePage({
    actorID,
    csrf,
    emojis,
    kind,
    onCompose,
    onOpenNote,
    onOpenProfile,
    refreshKey,
}: {
    actorID: string;
    csrf: string;
    emojis: Emoji[];
    kind: "home" | "public";
    onCompose: (kind: "reply" | "quote", note: Note) => void;
    onOpenNote: (noteID: string) => void;
    onOpenProfile: (actorID: string) => void;
    refreshKey: number;
}) {
    const [notes, setNotes] = useState<Note[]>([]);
    const [next, setNext] = useState("");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(
        async (after = "", append = false, signal?: AbortSignal) => {
            setLoading(true);
            setError("");
            try {
                const page = await api.timeline(kind, actorID, after, signal);
                setNotes((current) => (append ? [...current, ...page.data.filter((note) => !current.some((item) => item.id === note.id))] : page.data));
                setNext(page.next);
            } catch (reason) {
                if (!signal?.aborted) setError(reason instanceof Error ? reason.message : "タイムラインを読み込めませんでした");
            } finally {
                if (!signal?.aborted) setLoading(false);
            }
        },
        [actorID, kind],
    );
    useEffect(() => {
        void refreshKey;
        const controller = new AbortController();
        void load("", false, controller.signal);
        return () => controller.abort();
    }, [load, refreshKey]);
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
                <DividedList aria-label="ノート一覧">
                    {notes.map((note) => (
                        <NoteCard
                            emojis={emojis}
                            key={note.id}
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
                    ))}
                    {next && (
                        <div style={styles.loadMore}>
                            <Button disabled={loading} onClick={() => void load(next, true)} variant="secondary">
                                もっと見る
                            </Button>
                        </div>
                    )}
                </DividedList>
            )}
        </>
    );
}
