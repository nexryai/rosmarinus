import type { CSSProperties, ReactNode } from "react";

import { css } from "../lib/css";
import type { Actor, Note } from "../lib/schema";
import { Avatar } from "./ui";

type Quote = NonNullable<Note["quote"]>;

const styles = {
    card: {
        width: "100%",
        marginTop: "0.75rem",
        padding: "0.75rem",
        display: "flex",
        alignItems: "flex-start",
        gap: "0.625rem",
        overflow: "hidden",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        color: "var(--text)",
        background: "var(--panel)",
        textAlign: "left",
        transition: "border-color 140ms ease, background-color 140ms ease, transform 140ms ease",
    },
    content: {
        minWidth: 0,
        display: "block",
        flex: 1,
        color: "var(--text)",
        textAlign: "left",
    },
    header: {
        minWidth: 0,
        display: "flex",
        alignItems: "center",
        gap: "0.625rem",
    },
    identity: {
        minWidth: 0,
        flex: 1,
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
        flexShrink: 0,
        color: "var(--muted)",
        fontSize: "0.75rem",
    },
    text: {
        marginTop: "0.625rem",
        display: "-webkit-box",
        overflow: "hidden",
        WebkitBoxOrient: "vertical",
        WebkitLineClamp: 5,
        overflowWrap: "break-word",
        whiteSpace: "pre-wrap",
        fontSize: "0.875rem",
        lineHeight: "1.375rem",
    },
    warning: {
        marginTop: "0.625rem",
        padding: "0.5rem 0.625rem",
        display: "block",
        borderRadius: "0.625rem",
        color: "var(--muted)",
        background: "var(--panel-muted)",
        fontSize: "0.8125rem",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    card: css({
        "&:hover": {
            borderColor: "var(--accent-hover)",
            background: "var(--panel-muted)",
            transform: "translateY(-0.0625rem)",
        },
        "&:active": {
            transform: "translateY(0) scale(0.995)",
        },
        "@media (prefers-reduced-motion: reduce)": {
            transitionDuration: "0.01ms",
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

const quoteDate = (value: string) =>
    new Intl.DateTimeFormat("ja", {
        month: "short",
        day: "numeric",
    }).format(new Date(value));

export function QuotedNoteCard({ onOpen, onOpenProfile, quote }: { onOpen?: (noteID: string) => void; onOpenProfile?: (actorID: string) => void; quote: Quote }) {
    const author = quote.author;
    const authorName = author?.name || author?.username || "Unknown";
    const content: ReactNode = (
        <>
            <span style={styles.header}>
                <span style={styles.identity}>
                    <strong style={styles.name}>{authorName}</strong>
                    <span style={styles.handle}>{actorHandle(author)}</span>
                </span>
                <time dateTime={quote.created_at} style={styles.time}>
                    {quoteDate(quote.created_at)}
                </time>
            </span>
            {quote.content_warning ? <span style={styles.warning}>閲覧注意: {quote.content_warning}（詳細を開いて表示）</span> : <span style={styles.text}>{quote.text || "本文のないノート"}</span>}
        </>
    );

    return (
        <div className={rules.card} style={styles.card}>
            <Avatar actor={author} onOpenProfile={onOpenProfile} size="small" />
            {onOpen ? (
                <button aria-label={`引用ノートを開く: ${authorName}`} onClick={() => onOpen(quote.id)} style={styles.content} type="button">
                    {content}
                </button>
            ) : (
                <div style={styles.content}>{content}</div>
            )}
        </div>
    );
}
