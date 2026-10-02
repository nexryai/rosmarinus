import { type CSSProperties, useEffect, useRef, useState } from "react";

import { CustomEmoji, EmojiText } from "@/components/EmojiText";
import { Avatar, Button } from "@/components/ui";
import { api } from "@/lib/api";
import { css } from "@/lib/css";
import type { Note, ReactionActor } from "@/lib/schema";

const styles = {
    heading: { marginBottom: "1rem", fontSize: "0.875rem", fontWeight: 800 },
    groups: { display: "grid", gap: "1rem" },
    reaction: { display: "flex", alignItems: "center", gap: "0.5rem", marginBottom: "0.625rem", fontSize: "1rem" },
    count: { color: "var(--muted)", fontSize: "0.8125rem", fontWeight: 600 },
    actors: { display: "flex", flexWrap: "wrap", gap: "0.5rem", listStyle: "none", padding: 0, margin: 0 },
    actorItem: { minWidth: 0, maxWidth: "100%" },
    actor: { display: "flex", alignItems: "center", gap: "0.5rem", maxWidth: "100%", padding: "0.5rem 0.75rem", borderRadius: "0.75rem", background: "var(--panel-muted)", textAlign: "left" },
    identity: { display: "grid", minWidth: 0 },
    name: { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: "0.875rem", fontWeight: 700 },
    handle: { color: "var(--muted)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: "0.75rem" },
    status: { color: "var(--muted)", fontSize: "0.8125rem" },
    more: { marginTop: "0.625rem" },
} satisfies Record<string, CSSProperties>;

const rules = {
    section: css({
        padding: "1rem 1.25rem",
        borderBlock: "1px solid var(--border)",
        "@media (width >= 40rem)": { paddingInline: "1.75rem" },
    }),
    actor: css({
        "&:hover": { background: "var(--accent-soft)" },
        "&:focus-visible": { outline: "2px solid var(--accent-hover)", outlineOffset: "2px" },
    }),
};

function ReactionGroup({ actorID, noteID, summary, onOpenProfile }: { actorID: string; noteID: string; summary: Note["reactions"][number]; onOpenProfile: (actorID: string) => void }) {
    const [actors, setActors] = useState<ReactionActor[]>([]);
    const [next, setNext] = useState("");
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const controllerRef = useRef<AbortController | null>(null);
    const label = summary.reaction.replace("@.", "");

    useEffect(() => {
        const controller = new AbortController();
        controllerRef.current = controller;
        setLoading(true);
        setNext("");
        setError("");
        void api.noteReactionsPage(actorID, noteID, summary.reaction, { signal: controller.signal }).then(
            (page) => {
                if (controller.signal.aborted) return;
                setActors(page.data);
                setNext(page.next);
                setLoading(false);
            },
            () => {
                if (controller.signal.aborted) return;
                setError("リアクションしたユーザーを読み込めませんでした");
                setLoading(false);
            },
        );
        return () => controller.abort();
    }, [actorID, noteID, summary]);

    const loadMore = async () => {
        const signal = controllerRef.current?.signal;
        if (!signal || signal.aborted || loading) return;
        setLoading(true);
        setError("");
        try {
            const page = await api.noteReactionsPage(actorID, noteID, summary.reaction, { after: next || undefined, signal });
            if (signal.aborted) return;
            setActors((current) => (next ? Array.from(new Map([...current, ...page.data].map((actor) => [actor.id, actor])).values()) : page.data));
            setNext(page.next);
        } catch {
            if (!signal.aborted) setError("リアクションしたユーザーを読み込めませんでした");
        } finally {
            if (!signal.aborted) setLoading(false);
        }
    };

    return (
        <section aria-label={`${label}のリアクション`}>
            <h3 style={styles.reaction}>
                {summary.emoji ? <CustomEmoji emoji={summary.emoji} label={label} normal /> : <EmojiText text={label} />}
                <span style={styles.count}>{summary.count}件</span>
            </h3>
            <ul style={styles.actors}>
                {actors.map((actor) => (
                    <li key={actor.id} style={styles.actorItem}>
                        <button aria-label={`${actor.name || actor.username}のプロフィールを開く`} className={rules.actor} onClick={() => onOpenProfile(actor.id)} style={styles.actor} type="button">
                            <Avatar actor={actor} size="small" />
                            <span style={styles.identity}>
                                <span style={styles.name}>
                                    <EmojiText emojis={actor.emojis} text={actor.name || actor.username} />
                                </span>
                                <span style={styles.handle}>@{actor.username}</span>
                            </span>
                        </button>
                    </li>
                ))}
            </ul>
            {loading && (
                <p role="status" style={styles.status}>
                    読み込み中…
                </p>
            )}
            {error && (
                <p role="alert" style={styles.status}>
                    {error}
                </p>
            )}
            {!loading && !error && actors.length === 0 && <p style={styles.status}>表示できるユーザーはいません。</p>}
            {(next || error) && (
                <Button disabled={loading} onClick={() => void loadMore()} size="small" style={styles.more} variant="secondary">
                    {error ? "再試行" : "もっと見る"}
                </Button>
            )}
        </section>
    );
}

export function NoteReactions({ actorID, note, onOpenProfile }: { actorID: string; note: Pick<Note, "id" | "reactions">; onOpenProfile: (actorID: string) => void }) {
    return (
        <section aria-label="リアクション" className={rules.section}>
            <h2 style={styles.heading}>リアクション</h2>
            <div style={styles.groups}>{note.reactions.length > 0 ? note.reactions.map((summary) => <ReactionGroup actorID={actorID} key={`${actorID}:${note.id}:${summary.reaction}`} noteID={note.id} onOpenProfile={onOpenProfile} summary={summary} />) : <p style={styles.status}>まだリアクションはありません。</p>}</div>
        </section>
    );
}
