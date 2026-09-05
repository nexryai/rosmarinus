import { type CSSProperties, useState } from "react";

import { IconMessageCircle, IconQuote, IconRepeat, IconTrash } from "@tabler/icons-react";

import { css } from "../lib/css";
import type { Emoji, Note } from "../lib/schema";
import { Avatar, Button } from "./ui";

const styles = {
    card: { display: "flex", gap: "0.75rem", transition: "background-color 150ms" },
    body: { minWidth: 0, flex: 1 },
    header: { minWidth: 0, display: "flex", alignItems: "center", gap: "0.375rem", color: "var(--muted)", fontSize: "0.875rem" },
    actorLink: { minWidth: 0, overflow: "hidden", color: "inherit", background: "transparent", textAlign: "left", textOverflow: "ellipsis", whiteSpace: "nowrap" },
    actorName: { marginRight: "0.5rem", color: "var(--text)" },
    visibility: { padding: "0.125rem 0.375rem", borderRadius: "9999px", background: "var(--panel-muted)", fontSize: "10px" },
    detail: { marginLeft: "auto", padding: "0.25rem 0.5rem", borderRadius: "9999px", fontSize: "0.75rem", fontWeight: 700 },
    reference: { marginTop: "0.75rem", padding: "0.75rem", display: "grid", gridTemplateColumns: "auto 1fr", alignItems: "center", columnGap: "0.5rem", border: "1px solid var(--border)", borderRadius: "1rem", color: "var(--muted)", fontSize: "0.875rem" },
    referenceText: { marginTop: "0.5rem", color: "var(--text)", gridColumn: "span 2" },
    warning: { marginTop: "0.75rem", padding: "0.75rem", display: "flex", alignItems: "center", gap: "0.75rem", borderRadius: "0.75rem", background: "var(--panel-muted)", fontSize: "0.875rem" },
    warningButton: { minHeight: "2rem", marginLeft: "auto", paddingInline: "0.75rem", fontSize: "0.75rem" },
    text: { marginTop: "0.5rem", overflowWrap: "break-word", whiteSpace: "pre-wrap", lineHeight: "1.75rem" },
    emoji: { width: "1.5rem", height: "1.5rem", marginInline: "0.125rem", display: "inline-block", objectFit: "contain", verticalAlign: "text-bottom" },
    attachments: { maxHeight: "32rem", marginTop: "0.75rem", display: "grid", gap: "0.25rem", overflow: "hidden", borderRadius: "1rem" },
    attachmentImage: { width: "100%", height: "100%", maxHeight: "24rem", objectFit: "cover", background: "var(--panel-muted)" },
    attachmentFile: { padding: "1rem", display: "block", border: "1px solid var(--border)", borderRadius: "0.75rem", fontSize: "0.875rem", textDecoration: "underline" },
    quote: { marginTop: "0.75rem", padding: "0.75rem", border: "1px solid var(--border)", borderRadius: "1rem", fontSize: "0.875rem" },
    quoteText: { marginTop: "0.25rem", whiteSpace: "pre-wrap" },
    poll: { marginTop: "0.75rem", display: "grid", gap: "0.5rem" },
    pollButton: { position: "relative", width: "100%", padding: "0.625rem 1rem", display: "flex", overflow: "hidden", borderWidth: 1, borderStyle: "solid", borderRadius: "0.75rem", background: "var(--panel)", textAlign: "left", fontSize: "0.875rem" },
    pollText: { position: "relative", zIndex: 10 },
    pollVotes: { position: "relative", zIndex: 10, marginLeft: "auto", fontWeight: 700 },
    pollBar: { position: "absolute", insetBlock: 0, left: 0, opacity: 0.6, background: "var(--accent-soft)" },
    actions: { marginTop: "1rem", display: "flex", alignItems: "center", flexWrap: "wrap", gap: "0.5rem" },
    action: { minWidth: "2rem", minHeight: "2rem", paddingInline: "0.5rem", display: "grid", placeItems: "center", border: "1px solid transparent", borderRadius: "9999px", transition: "color 150ms, background-color 150ms" },
    reaction: { display: "flex", gap: "0.375rem", borderColor: "var(--border)" },
    reactionActive: { color: "var(--accent-ink)", borderColor: "var(--accent)", background: "var(--accent-soft)" },
    reactionCount: { fontSize: "0.75rem", fontVariantNumeric: "tabular-nums" },
    deleteAction: { marginLeft: "auto" },
    picker: { maxHeight: "8rem", marginTop: "0.5rem", padding: "0.5rem", display: "flex", flexWrap: "wrap", gap: "0.25rem", overflowY: "auto", border: "1px solid var(--border)", borderRadius: "1rem", background: "var(--panel)" },
    pickerButton: { width: "2.25rem", height: "2.25rem", display: "grid", placeItems: "center", borderRadius: "0.75rem", fontSize: "1.25rem" },
    pickerImage: { width: "1.5rem", height: "1.5rem", objectFit: "contain" },
} satisfies Record<string, CSSProperties>;

const rules = {
    card: css({ paddingInline: "1.25rem", paddingBlock: "1.25rem", "&:hover": { background: "var(--panel-muted)" }, "@media (width >= 40rem)": { paddingInline: "1.75rem" }, ':root[data-compact="true"] &': { paddingBlock: "0.75rem" } }),
    detail: css({ color: "var(--muted)", "&:hover": { color: "var(--accent-hover)", background: "var(--accent-soft)" } }),
    reference: css({ "& > svg": { width: "1rem", height: "1rem" } }),
    pollButton: css({ borderColor: "var(--border)", '&[aria-pressed="true"]': { borderColor: "var(--accent-hover)" } }),
    action: css({ color: "var(--muted)", "&:hover": { color: "var(--accent-hover)", background: "var(--accent-soft)" }, "& > svg": { width: "1rem", height: "1rem" } }),
    deleteAction: css({ "&:hover": { color: "var(--danger)" } }),
    pickerButton: css({ "&:hover": { background: "var(--accent-soft)" } }),
};

const relativeTime = (value: string) => {
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 60) return `${seconds}秒`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}分`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}時間`;
    return new Intl.DateTimeFormat("ja", { month: "short", day: "numeric" }).format(new Date(value));
};

const renderText = (text: string, note: Note) => {
    const byName = new Map(note.emojis.map((emoji) => [emoji.name, emoji]));
    let offset = 0;
    return text.split(/(:[A-Za-z0-9_+-]+:)/g).map((part) => {
        const tokenOffset = offset;
        offset += part.length;
        const emoji = part.startsWith(":") && part.endsWith(":") ? byName.get(part.slice(1, -1)) : undefined;
        return emoji ? <img alt={part} key={tokenOffset} loading="lazy" src={emoji.url} style={styles.emoji} /> : part;
    });
};

export function NoteCard({
    note,
    ownActorID,
    onDelete,
    onOpenNote,
    onOpenProfile,
    onQuote,
    onReact,
    onRenote,
    onReply,
    onVote,
    emojis = [],
}: {
    note: Note;
    ownActorID: string;
    onDelete: (noteID: string) => Promise<void>;
    onOpenNote?: (noteID: string) => void;
    onOpenProfile: (actorID: string) => void;
    onQuote: (note: Note) => void;
    onReact: (noteID: string, reaction: string, reacted: boolean) => Promise<void>;
    onRenote: (note: Note) => Promise<void>;
    onReply: (note: Note) => void;
    onVote: (noteID: string, choice: number) => Promise<void>;
    emojis?: Emoji[];
}) {
    const [revealed, setRevealed] = useState(!note.content_warning);
    const [busy, setBusy] = useState(false);
    const [pickerOpen, setPickerOpen] = useState(false);
    const author = note.author;
    const maxPollVotes = Math.max(...(note.poll?.choices ?? []).map((item) => item.votes), 1);
    const act = async (operation: () => Promise<void>) => {
        setBusy(true);
        try {
            await operation();
        } finally {
            setBusy(false);
        }
    };
    return (
        <article className={rules.card} style={styles.card}>
            <Avatar actor={author} />
            <div style={styles.body}>
                <header style={styles.header}>
                    <button disabled={!author} onClick={() => author && onOpenProfile(author.id)} style={styles.actorLink} type="button">
                        <strong style={styles.actorName}>{author?.name || author?.username || "Unknown"}</strong>
                        <span>@{author?.username || "unknown"}</span>
                    </button>
                    <span>·</span>
                    <time dateTime={note.created_at}>{relativeTime(note.created_at)}</time>
                    <span style={styles.visibility}>{note.visibility === "followers" ? "フォロワー" : note.visibility === "home" ? "ホーム" : note.visibility === "specified" ? "宛先指定" : "公開"}</span>
                    {onOpenNote && (
                        <button className={rules.detail} onClick={() => onOpenNote(note.id)} style={styles.detail} type="button">
                            詳細
                        </button>
                    )}
                </header>
                {note.renote && (
                    <div className={rules.reference} style={styles.reference}>
                        <IconRepeat />
                        <span>{note.renote.author?.name || note.renote.author?.username}</span>
                        <p style={styles.referenceText}>{note.renote.text}</p>
                    </div>
                )}
                {note.content_warning && (
                    <div style={styles.warning}>
                        <strong>{note.content_warning}</strong>
                        <Button onClick={() => setRevealed((value) => !value)} style={styles.warningButton} variant="secondary">
                            {revealed ? "隠す" : "表示"}
                        </Button>
                    </div>
                )}
                {revealed && note.text && <p style={styles.text}>{renderText(note.text, note)}</p>}
                {revealed && note.attachments.length > 0 && (
                    <div style={{ ...styles.attachments, ...(note.attachments.length > 1 ? { gridTemplateColumns: "repeat(2, minmax(0, 1fr))" } : {}) }}>
                        {note.attachments.map((attachment) =>
                            attachment.media_type?.startsWith("image/") ? (
                                <a href={attachment.url} key={attachment.url} rel="noreferrer" target="_blank">
                                    <img alt={attachment.name || "添付画像"} loading="lazy" referrerPolicy="no-referrer" src={attachment.url} style={styles.attachmentImage} />
                                </a>
                            ) : (
                                <a href={attachment.url} key={attachment.url} rel="noreferrer" style={styles.attachmentFile} target="_blank">
                                    {attachment.name || "添付ファイル"}
                                </a>
                            ),
                        )}
                    </div>
                )}
                {note.quote && (
                    <div style={styles.quote}>
                        <strong>{note.quote.author?.name || note.quote.author?.username}</strong>
                        <p style={styles.quoteText}>{note.quote.text}</p>
                    </div>
                )}
                {note.poll && (
                    <div style={styles.poll}>
                        {note.poll.choices.map((choice) => (
                            <button aria-pressed={choice.voted} className={rules.pollButton} disabled={busy || note.poll?.expired} key={choice.index} onClick={() => act(() => onVote(note.id, choice.index))} style={styles.pollButton} type="button">
                                <span style={styles.pollText}>{choice.text}</span>
                                <span style={styles.pollVotes}>{choice.votes}票</span>
                                <i style={{ ...styles.pollBar, width: `${Math.max(3, choice.votes ? (choice.votes / maxPollVotes) * 100 : 3)}%` }} />
                            </button>
                        ))}
                    </div>
                )}
                <footer style={styles.actions}>
                    <button aria-label="返信" className={rules.action} onClick={() => onReply(note)} style={styles.action} type="button">
                        <IconMessageCircle />
                    </button>
                    <button aria-label="リノート" className={rules.action} disabled={busy} onClick={() => void act(() => onRenote(note))} style={styles.action} type="button">
                        <IconRepeat />
                    </button>
                    <button aria-label="引用" className={rules.action} onClick={() => onQuote(note)} style={styles.action} type="button">
                        <IconQuote />
                    </button>
                    {note.reactions.map((reaction) => (
                        <button
                            aria-pressed={reaction.reacted}
                            className={rules.action}
                            disabled={busy}
                            key={reaction.reaction}
                            onClick={() => act(() => onReact(note.id, reaction.reaction, reaction.reacted))}
                            style={{ ...styles.action, ...styles.reaction, ...(reaction.reacted ? styles.reactionActive : {}) }}
                            type="button"
                        >
                            <span>{reaction.reaction}</span>
                            <b style={styles.reactionCount}>{reaction.count}</b>
                        </button>
                    ))}
                    <button aria-label="リアクションを追加" className={rules.action} disabled={busy} onClick={() => setPickerOpen((value) => !value)} style={styles.action} type="button">
                        ＋
                    </button>
                    {author?.id === ownActorID && (
                        <button
                            aria-label="削除"
                            className={`${rules.action} ${rules.deleteAction}`}
                            disabled={busy}
                            onClick={() => {
                                if (window.confirm("このノートを削除しますか？")) void act(() => onDelete(note.id));
                            }}
                            style={{ ...styles.action, ...styles.deleteAction }}
                            type="button"
                        >
                            <IconTrash />
                        </button>
                    )}
                </footer>
                {pickerOpen && (
                    <div style={styles.picker}>
                        <button className={rules.pickerButton} onClick={() => void act(() => onReact(note.id, "👍", false)).then(() => setPickerOpen(false))} style={styles.pickerButton} type="button">
                            👍
                        </button>
                        <button className={rules.pickerButton} onClick={() => void act(() => onReact(note.id, "❤️", false)).then(() => setPickerOpen(false))} style={styles.pickerButton} type="button">
                            ❤️
                        </button>
                        <button className={rules.pickerButton} onClick={() => void act(() => onReact(note.id, "😂", false)).then(() => setPickerOpen(false))} style={styles.pickerButton} type="button">
                            😂
                        </button>
                        {emojis.map((emoji) => (
                            <button aria-label={`:${emoji.name}:`} className={rules.pickerButton} key={emoji.name} onClick={() => void act(() => onReact(note.id, `:${emoji.name}:`, false)).then(() => setPickerOpen(false))} style={styles.pickerButton} title={`:${emoji.name}:`} type="button">
                                <img alt="" src={emoji.url} style={styles.pickerImage} />
                            </button>
                        ))}
                    </div>
                )}
            </div>
        </article>
    );
}
