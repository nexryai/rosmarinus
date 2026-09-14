import type { CSSProperties, ReactNode } from "react";

import { IconPhoto } from "@tabler/icons-react";

import { css } from "../lib/css";
import type { Actor, Note } from "../lib/schema";
import { EmojiText } from "./EmojiText";
import { Mfm } from "./Mfm";
import { Avatar } from "./ui";

export type ThreadNoteData = Note | NonNullable<Note["reply"]>;

const styles = {
    root: {
        width: "100%",
        maxWidth: "64rem",
        marginInline: "auto",
        padding: "0.875rem 1.25rem",
        display: "grid",
        gridTemplateColumns: "2.25rem minmax(0, 1fr)",
        gap: "0.625rem",
        position: "relative",
    },
    rail: {
        position: "relative",
    },
    lineBefore: {
        width: "1px",
        height: "0.875rem",
        position: "absolute",
        top: "-0.875rem",
        left: "1.125rem",
        background: "var(--border)",
    },
    lineAfter: {
        width: "1px",
        position: "absolute",
        top: "2.25rem",
        bottom: "-0.875rem",
        left: "1.125rem",
        background: "var(--border)",
    },
    body: {
        minWidth: 0,
        color: "var(--text)",
    },
    content: {
        width: "100%",
        minWidth: 0,
        display: "block",
        color: "inherit",
        textAlign: "left",
    },
    header: {
        minWidth: 0,
        display: "flex",
        alignItems: "baseline",
        gap: "0.375rem",
        overflow: "hidden",
        whiteSpace: "nowrap",
    },
    name: {
        overflow: "hidden",
        fontSize: "0.875rem",
        textOverflow: "ellipsis",
    },
    handle: {
        overflow: "hidden",
        color: "var(--muted)",
        fontSize: "0.75rem",
        textOverflow: "ellipsis",
    },
    time: {
        marginLeft: "auto",
        flexShrink: 0,
        color: "var(--muted)",
        fontSize: "0.75rem",
    },
    text: {
        marginTop: "0.25rem",
        display: "-webkit-box",
        overflow: "hidden",
        WebkitBoxOrient: "vertical",
        WebkitLineClamp: 4,
        overflowWrap: "break-word",
        whiteSpace: "pre-wrap",
        fontSize: "0.875rem",
        lineHeight: "1.375rem",
    },
    warning: {
        marginTop: "0.375rem",
        padding: "0.375rem 0.5rem",
        display: "block",
        borderRadius: "0.5rem",
        color: "var(--muted)",
        background: "var(--panel-muted)",
        fontSize: "0.8125rem",
    },
    media: {
        marginTop: "0.375rem",
        display: "inline-flex",
        alignItems: "center",
        gap: "0.25rem",
        color: "var(--muted)",
        fontSize: "0.75rem",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    content: css({
        borderRadius: "0.5rem",
        "&:hover": {
            color: "var(--accent-hover)",
        },
        "&:focus-visible": {
            outline: "2px solid var(--focus)",
            outlineOffset: "0.25rem",
        },
    }),
};

const actorHandle = (actor?: Actor) => {
    const username = actor?.username || "unknown";
    if (!actor?.uri) return `@${username}`;
    try {
        const host = new URL(actor.uri).hostname;
        return host === window.location.hostname ? `@${username}` : `@${username}@${host}`;
    } catch {
        return `@${username}`;
    }
};

const noteDate = (value: string) =>
    new Intl.DateTimeFormat("ja", {
        month: "short",
        day: "numeric",
    }).format(new Date(value));

export function ThreadNote({
    className = "",
    label,
    lineAfter = false,
    lineBefore = false,
    note,
    onOpenNote,
    onOpenProfile,
    style,
}: {
    className?: string;
    label: string;
    lineAfter?: boolean;
    lineBefore?: boolean;
    note: ThreadNoteData;
    onOpenNote?: (noteID: string) => void;
    onOpenProfile?: (actorID: string) => void;
    style?: CSSProperties;
}) {
    const author = note.author;
    const authorName = author?.name || author?.username || "Unknown";
    const imageCount = note.attachments.filter((attachment) => attachment.media_type?.startsWith("image/")).length;
    const content: ReactNode = (
        <>
            <span style={styles.header}>
                <strong style={styles.name}>
                    <EmojiText emojis={author?.emojis} text={authorName} />
                </strong>
                <span style={styles.handle}>{actorHandle(author)}</span>
                <time dateTime={note.created_at} style={styles.time} title={new Date(note.created_at).toLocaleString("ja")}>
                    {noteDate(note.created_at)}
                </time>
            </span>
            {note.content_warning ? <span style={styles.warning}>閲覧注意: {note.content_warning}（詳細を開いて表示）</span> : <Mfm emojis={note.emojis} style={styles.text} text={note.text || "本文のないノート"} />}
            {imageCount > 0 && (
                <span style={styles.media}>
                    <IconPhoto aria-hidden="true" size={15} />
                    画像 {imageCount}件
                </span>
            )}
        </>
    );

    return (
        <article aria-label={label} className={className} style={{ ...styles.root, ...style }}>
            <div style={styles.rail}>
                {lineBefore && <i aria-hidden="true" style={styles.lineBefore} />}
                {lineAfter && <i aria-hidden="true" style={styles.lineAfter} />}
                <Avatar actor={author} onOpenProfile={onOpenProfile} size="small" />
            </div>
            <div style={styles.body}>
                {onOpenNote ? (
                    <button aria-label={`${label}を開く: ${authorName}`} className={rules.content} onClick={() => onOpenNote(note.id)} style={styles.content} type="button">
                        {content}
                    </button>
                ) : (
                    <div style={styles.content}>{content}</div>
                )}
            </div>
        </article>
    );
}
