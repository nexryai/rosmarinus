import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconArrowLeft } from "@tabler/icons-react";

import { NoteCard } from "../components/NoteCard";
import { DividedList, ErrorBanner, Loading, PageHeader, RoundButton } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { Emoji, Note } from "../lib/schema";

const styles = {
    back: { marginRight: "0.75rem", marginLeft: 0 },
} satisfies Record<string, CSSProperties>;

const rules = {
    thread: css({ "& > h2": { padding: "0.75rem 1.25rem", fontSize: "0.875rem", fontWeight: 900, background: "var(--panel-muted)" }, "@media (width >= 40rem)": { "& > h2": { paddingInline: "1.75rem" } } }),
};

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
    const load = useCallback(
        async (signal?: AbortSignal) => {
            setLoading(true);
            setError("");
            try {
                const [root, replies] = await Promise.all([api.note(actorID, noteID, signal), api.thread(actorID, noteID, signal)]);
                setNote(root);
                setThread(replies.filter((item) => item.id !== root.id));
            } catch (reason) {
                if (!signal?.aborted) setError(reason instanceof Error ? reason.message : "ノートを読み込めませんでした");
            } finally {
                if (!signal?.aborted) setLoading(false);
            }
        },
        [actorID, noteID],
    );
    useEffect(() => {
        void refreshKey;
        const controller = new AbortController();
        void load(controller.signal);
        return () => controller.abort();
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
            <PageHeader
                eyebrow="会話"
                leading={
                    <RoundButton aria-label="戻る" onClick={onBack} style={styles.back}>
                        <IconArrowLeft />
                    </RoundButton>
                }
                title="ノート"
            />
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            {loading && !note ? (
                <Loading label="ノートを読み込み中" />
            ) : (
                note && (
                    <DividedList aria-label="スレッド" className={rules.thread}>
                        {card(note)}
                        {thread.length > 0 && <h2>返信</h2>}
                        {thread.map(card)}
                    </DividedList>
                )
            )}
        </>
    );
}
