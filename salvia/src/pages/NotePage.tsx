import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconArrowLeft } from "@tabler/icons-react";

import { NoteCard } from "../components/NoteCard";
import { NoteList, NoteListItem } from "../components/NoteList";
import { ThreadNote } from "../components/ThreadNote";
import { ErrorBanner, Loading, PageHeader, RoundButton } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";
import type { Emoji, Note } from "../lib/schema";

const styles = {
    back: {
        marginRight: "0.75rem",
        marginLeft: 0,
    },
    branch: {
        maxWidth: "64rem",
        marginInline: "auto",
    },
    nestedBranch: {
        marginLeft: "0.75rem",
        borderLeft: "1px solid var(--border)",
    },
    continue: {
        margin: "0.25rem 1.25rem 0.75rem 4.25rem",
        color: "var(--accent-hover)",
        fontSize: "0.8125rem",
        fontWeight: 700,
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    thread: css({
        "& > h2": {
            padding: "0.75rem 1.25rem",
            fontSize: "0.875rem",
            fontWeight: 900,
            background: "var(--panel-muted)",
        },
        "@media (width >= 40rem)": {
            "& > h2": {
                paddingInline: "1.75rem",
            },
        },
    }),
};

function ThreadBranch({ actorID, depth, note, onOpenNote, onOpenProfile, refreshKey }: { actorID: string; depth: number; note: Note; onOpenNote: (noteID: string) => void; onOpenProfile: (actorID: string) => void; refreshKey: number }) {
    const [children, setChildren] = useState<Note[]>([]);
    useEffect(() => {
        void refreshKey;
        if (depth >= 5) return;
        const controller = new AbortController();
        void api
            .thread(actorID, note.id, controller.signal, 5)
            .then(setChildren)
            .catch((reason) => {
                if (!controller.signal.aborted) console.error("Failed to load nested Note replies", reason);
            });
        return () => controller.abort();
    }, [actorID, depth, note.id, refreshKey]);

    return (
        <div style={{ ...styles.branch, ...(depth > 1 ? styles.nestedBranch : {}) }}>
            <ThreadNote label="返信ノート" lineAfter={children.length > 0} lineBefore note={note} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} />
            {depth < 5 ? (
                children.map((child) => <ThreadBranch actorID={actorID} depth={depth + 1} key={child.id} note={child} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} refreshKey={refreshKey} />)
            ) : (
                <button onClick={() => onOpenNote(note.id)} style={styles.continue} type="button">
                    この先のスレッドを表示
                </button>
            )}
        </div>
    );
}

async function loadConversation(actorID: string, root: Note, signal?: AbortSignal) {
    const ancestors: Note[] = [];
    const seen = new Set([root.id]);
    let parentID = root.reply_id;
    while (parentID && ancestors.length < 100 && !seen.has(parentID)) {
        seen.add(parentID);
        const parent = await api.note(actorID, parentID, signal);
        ancestors.unshift(parent);
        parentID = parent.reply_id;
    }
    return ancestors;
}

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
    const [conversation, setConversation] = useState<Note[]>([]);
    const [thread, setThread] = useState<Note[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const load = useCallback(
        async (signal?: AbortSignal) => {
            setLoading(true);
            setError("");
            try {
                const root = await api.note(actorID, noteID, signal);
                const [ancestors, replies] = await Promise.all([loadConversation(actorID, root, signal), api.thread(actorID, noteID, signal, 30)]);
                setNote(root);
                setConversation(ancestors);
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
                    <NoteList aria-label="スレッド" className={rules.thread}>
                        {conversation.map((ancestor, index) => (
                            <ThreadNote key={ancestor.id} label="会話の前のノート" lineAfter lineBefore={index > 0} note={ancestor} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} />
                        ))}
                        <NoteListItem key={note.id}>
                            <NoteCard
                                emojis={emojis}
                                note={note}
                                onDelete={(id) => mutate(() => api.deletePost(csrf, actorID, id))}
                                onOpenNote={onOpenNote}
                                onOpenProfile={onOpenProfile}
                                onQuote={(target) => onCompose("quote", target)}
                                onReact={(id, reaction, reacted) => mutate(() => (reacted ? api.unreact(csrf, actorID, id) : api.react(csrf, actorID, id, reaction)))}
                                onRenote={(target) => mutate(() => api.createPost(csrf, actorID, { renote_id: target.id, visibility: target.visibility }))}
                                onReply={(target) => onCompose("reply", target)}
                                onVote={(id, choice) => mutate(() => api.vote(csrf, actorID, id, choice))}
                                ownActorID={actorID}
                                showReplyContext={false}
                            />
                        </NoteListItem>
                        {thread.length > 0 && <h2>返信</h2>}
                        {thread.map((reply) => (
                            <ThreadBranch actorID={actorID} depth={1} key={reply.id} note={reply} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} refreshKey={refreshKey} />
                        ))}
                    </NoteList>
                )
            )}
        </>
    );
}
