import { useCallback, useEffect, useState } from "react";

import { IconArrowLeft } from "@tabler/icons-react";

import { NoteCard } from "../components/NoteCard";
import { ErrorBanner, Loading } from "../components/ui";
import { api } from "../lib/api";
import type { Emoji, Note } from "../lib/schema";

export function NotePage({
    actorID,
    csrf,
    emojis,
    noteID,
    onBack,
    onCompose,
    onOpenNote,
    onOpenProfile,
    refreshKey,
}: {
    actorID: string;
    csrf: string;
    emojis: Emoji[];
    noteID: string;
    onBack: () => void;
    onCompose: (kind: "reply" | "quote", note: Note) => void;
    onOpenNote: (noteID: string) => void;
    onOpenProfile: (actorID: string) => void;
    refreshKey: number;
}) {
    const [note, setNote] = useState<Note>();
    const [thread, setThread] = useState<Note[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            const [root, replies] = await Promise.all([api.note(actorID, noteID), api.thread(actorID, noteID)]);
            setNote(root);
            setThread(replies.filter((item) => item.id !== root.id));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "ノートを読み込めませんでした");
        } finally {
            setLoading(false);
        }
    }, [actorID, noteID]);
    useEffect(() => {
        void refreshKey;
        void load();
    }, [load, refreshKey]);
    const mutate = async (operation: () => Promise<void>) => {
        try {
            await operation();
            await load();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "操作に失敗しました");
        }
    };
    const card = (item: Note) => (
        <NoteCard
            emojis={emojis}
            key={item.id}
            note={item}
            onDelete={(id) => mutate(() => api.deletePost(csrf, actorID, id))}
            onOpenNote={onOpenNote}
            onOpenProfile={onOpenProfile}
            onQuote={(target) => onCompose("quote", target)}
            onReact={(id, reaction, reacted) => mutate(() => (reacted ? api.unreact(csrf, actorID, id) : api.react(csrf, actorID, id, reaction)))}
            onRenote={(target) => mutate(() => api.createPost(csrf, actorID, { renote_id: target.id, visibility: target.visibility }))}
            onReply={(target) => onCompose("reply", target)}
            onVote={(id, choice) => mutate(() => api.vote(csrf, actorID, id, choice))}
            ownActorID={actorID}
        />
    );
    return (
        <>
            <header className="page-header">
                <button aria-label="戻る" className="round-button round-button--leading" onClick={onBack} type="button">
                    <IconArrowLeft />
                </button>
                <div>
                    <p className="eyebrow">会話</p>
                    <h1>ノート</h1>
                </div>
            </header>
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading && !note ? (
                <Loading label="ノートを読み込み中" />
            ) : (
                note && (
                    <section aria-label="スレッド" className="feed note-thread">
                        {card(note)}
                        {thread.length > 0 && <h2>返信</h2>}
                        {thread.map(card)}
                    </section>
                )
            )}
        </>
    );
}
