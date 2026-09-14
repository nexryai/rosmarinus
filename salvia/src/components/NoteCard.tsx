import { type CSSProperties, useEffect, useState } from "react";

import { IconHome, IconLock, IconMail, IconMessageCircle, IconPinFilled, IconQuote, IconRepeat, IconTrash, IconWorld } from "@tabler/icons-react";

import { css } from "../lib/css";
import type { Emoji, Note } from "../lib/schema";
import { CustomEmoji, EmojiText } from "./EmojiText";
import { ImageViewer } from "./ImageViewer";
import { Mfm } from "./Mfm";
import { NoteMedia } from "./NoteMedia";
import { NoteSurface } from "./NoteSurface";
import { QuotedNoteCard } from "./QuotedNoteCard";
import { ThreadNote } from "./ThreadNote";
import { Avatar, Button } from "./ui";
import { ConfirmDialog } from "./ui/ConfirmDialog";

const styles = {
    body: {
        minWidth: 0,
        flex: 1,
    },
    header: {
        minWidth: 0,
        display: "flex",
        alignItems: "center",
        gap: "0.375rem",
        color: "var(--muted)",
        fontSize: "0.875rem",
    },
    actorLink: {
        minWidth: 0,
        overflow: "hidden",
        color: "inherit",
        background: "transparent",
        textAlign: "left",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    actorName: {
        marginRight: "0.5rem",
        color: "var(--text)",
    },
    visibility: {
        display: "grid",
        placeItems: "center",
        flexShrink: 0,
        color: "var(--muted)",
    },
    timestamp: {
        marginLeft: "auto",
        padding: "0.25rem 0.5rem",
        borderRadius: "9999px",
        color: "var(--muted)",
        fontSize: "0.75rem",
        whiteSpace: "nowrap",
    },
    pinned: {
        paddingInline: "0.375rem",
        gridColumn: "1 / -1",
        display: "flex",
        alignItems: "center",
        gap: "0.375rem",
        color: "var(--pinned)",
        fontSize: "0.8125rem",
        fontWeight: 700,
    },
    renoteAttribution: {
        minWidth: 0,
        paddingInline: "0.375rem",
        gridColumn: "1 / -1",
        display: "flex",
        alignItems: "center",
        gap: "0.5rem",
        color: "var(--renote)",
        fontSize: "0.8125rem",
        fontWeight: 700,
    },
    renoteActor: {
        minWidth: 0,
        overflow: "hidden",
        color: "inherit",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    renoteTime: {
        marginLeft: "auto",
        flexShrink: 0,
        color: "var(--muted)",
        fontWeight: 400,
    },
    deleted: {
        minHeight: "3.5rem",
        gridColumn: "1 / -1",
        display: "grid",
        placeItems: "center",
        borderRadius: "0.75rem",
        color: "var(--muted)",
        background: "var(--panel-muted)",
        fontSize: "0.875rem",
    },
    warning: {
        marginTop: "0.75rem",
        padding: "0.75rem",
        display: "flex",
        alignItems: "center",
        gap: "0.75rem",
        borderRadius: "0.75rem",
        background: "var(--panel-muted)",
        fontSize: "0.875rem",
    },
    warningButton: {
        minHeight: "2rem",
        marginLeft: "auto",
        paddingInline: "0.75rem",
        fontSize: "0.75rem",
    },
    text: {
        marginTop: "0.5rem",
        display: "block",
        overflowWrap: "break-word",
        whiteSpace: "pre-wrap",
        lineHeight: "1.75rem",
    },
    poll: {
        marginTop: "0.75rem",
        display: "grid",
        gap: "0.5rem",
    },
    pollButton: {
        position: "relative",
        width: "100%",
        padding: "0.625rem 1rem",
        display: "flex",
        overflow: "hidden",
        borderWidth: 1,
        borderStyle: "solid",
        borderRadius: "0.75rem",
        background: "var(--panel)",
        textAlign: "left",
        fontSize: "0.875rem",
    },
    pollText: {
        position: "relative",
        zIndex: 10,
    },
    pollVotes: {
        position: "relative",
        zIndex: 10,
        marginLeft: "auto",
        fontWeight: 700,
    },
    pollBar: {
        position: "absolute",
        insetBlock: 0,
        left: 0,
        opacity: 0.6,
        background: "var(--accent-soft)",
    },
    actions: {
        marginTop: "1rem",
        display: "flex",
        alignItems: "center",
        flexWrap: "wrap",
        gap: "0.5rem",
    },
    action: {
        minWidth: "2rem",
        minHeight: "2rem",
        paddingInline: "0.5rem",
        display: "grid",
        placeItems: "center",
        border: "1px solid transparent",
        borderRadius: "9999px",
        transition: "color 150ms, background-color 150ms",
    },
    reaction: {
        display: "flex",
        gap: "0.375rem",
        borderColor: "var(--border)",
    },
    reactionActive: {
        color: "var(--accent-ink)",
        borderColor: "var(--accent)",
        background: "var(--accent-soft)",
    },
    reactionCount: {
        fontSize: "0.75rem",
        fontVariantNumeric: "tabular-nums",
    },
    replyAction: {
        display: "flex",
        gap: "0.25rem",
    },
    deleteAction: {
        marginLeft: "auto",
    },
    picker: {
        maxHeight: "8rem",
        marginTop: "0.5rem",
        padding: "0.5rem",
        display: "flex",
        flexWrap: "wrap",
        gap: "0.25rem",
        overflowY: "auto",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        background: "var(--panel)",
    },
    pickerButton: {
        width: "2.25rem",
        height: "2.25rem",
        display: "grid",
        placeItems: "center",
        borderRadius: "0.75rem",
        fontSize: "1.25rem",
    },
    pickerImage: {
        width: "1.5rem",
        height: "1.5rem",
        objectFit: "contain",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    timestamp: css({
        color: "var(--muted)",
        "&:hover": {
            color: "var(--accent-hover)",
            background: "var(--accent-soft)",
        },
    }),
    visibility: css({
        "& > svg": {
            width: "1rem",
            height: "1rem",
        },
    }),
    pinned: css({
        "& > svg": {
            width: "1rem",
            height: "1rem",
        },
    }),
    renoteAttribution: css({
        "& > svg": {
            width: "1rem",
            height: "1rem",
            flexShrink: 0,
        },
        "& button:hover": {
            textDecoration: "underline",
        },
    }),
    pollButton: css({
        borderColor: "var(--border)",
        '&[aria-pressed="true"]': {
            borderColor: "var(--accent-hover)",
        },
    }),
    action: css({
        color: "var(--muted)",
        "&:hover": {
            color: "var(--accent-hover)",
            background: "var(--accent-soft)",
        },
        "& > svg": {
            width: "1rem",
            height: "1rem",
        },
    }),
    deleteAction: css({
        "&:hover": {
            color: "var(--danger)",
        },
    }),
    pickerButton: css({
        "&:hover": {
            background: "var(--accent-soft)",
        },
    }),
};

const relativeTime = (value: string) => {
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 60) return `${seconds}秒`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}分`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}時間`;
    return new Intl.DateTimeFormat("ja", { month: "short", day: "numeric" }).format(new Date(value));
};

const visibilityDetails = (visibility: string) => {
    if (visibility === "home") return { icon: IconHome, label: "ホーム" };
    if (visibility === "followers") return { icon: IconLock, label: "フォロワー" };
    if (visibility === "specified") return { icon: IconMail, label: "宛先指定" };
    return { icon: IconWorld, label: "公開" };
};

const actorHandle = (actor: Note["author"]) => {
    const username = actor?.username || "unknown";
    if (!actor?.uri) return `@${username}`;
    try {
        const host = new URL(actor.uri).hostname;
        return host === window.location.hostname ? `@${username}` : `@${username}@${host}`;
    } catch {
        return `@${username}`;
    }
};

const displayedNoteFor = (note: Note): Note => {
    if (!note.renote) return note;
    return {
        ...note,
        ...note.renote,
        mention_uris: [],
        hashtags: [],
        published_at: null,
        reactions: note.renote.reactions,
        poll: undefined,
        reply_id: note.renote.reply?.id,
        quote_id: note.renote.quote?.id,
        renote_id: undefined,
        reply: note.renote.reply,
        quote: note.renote.quote,
        renote: undefined,
    };
};

const canonicalReaction = (reaction: string, emojis: Emoji[]) => {
    const match = /^:([A-Za-z0-9_]+):$/.exec(reaction);
    return match && emojis.some((emoji) => emoji.name === match[1]) ? `:${match[1]}@.:` : reaction;
};

const changedReactions = (reactions: Note["reactions"], requestedReaction: string, reacted: boolean, emojis: Emoji[]) => {
    const reaction = canonicalReaction(requestedReaction, emojis);
    if (reacted) return reactions.map((item) => (item.reaction === reaction ? { ...item, count: Math.max(0, item.count - 1), reacted: false } : item)).filter((item) => item.count > 0);

    const alreadyReacted = reactions.some((item) => item.reaction === reaction && item.reacted);
    const next = reactions.map((item) => (item.reacted && item.reaction !== reaction ? { ...item, count: Math.max(0, item.count - 1), reacted: false } : item)).filter((item) => item.count > 0);
    if (alreadyReacted) return next;
    const existing = next.find((item) => item.reaction === reaction);
    if (existing) return next.map((item) => (item.reaction === reaction ? { ...item, count: item.count + 1, reacted: true } : item));
    const localEmoji = emojis.find((emoji) => reaction === `:${emoji.name}@.:`);
    return [...next, { reaction, count: 1, reacted: true, emoji: localEmoji }];
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
    pinned = false,
    showReplyContext = true,
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
    pinned?: boolean;
    showReplyContext?: boolean;
}) {
    const displayedNote = displayedNoteFor(note);
    const isRenote = Boolean(note.renote_id || note.renote);
    const renoter = isRenote ? note.author : undefined;
    const renoteUnavailable = isRenote && !note.renote;
    const [revealed, setRevealed] = useState(!displayedNote.content_warning);
    const [busy, setBusy] = useState(false);
    const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
    const [pickerOpen, setPickerOpen] = useState(false);
    const [viewerIndex, setViewerIndex] = useState<number | undefined>(undefined);
    const [reactions, setReactions] = useState(displayedNote.reactions);
    useEffect(() => setReactions(displayedNote.reactions), [displayedNote.reactions]);
    const author = displayedNote.author;
    const visibility = visibilityDetails(displayedNote.visibility);
    const VisibilityIcon = visibility.icon;
    const imageAttachments = displayedNote.attachments.filter((attachment) => attachment.media_type?.startsWith("image/"));
    const maxPollVotes = Math.max(...(displayedNote.poll?.choices ?? []).map((item) => item.votes), 1);
    const act = async (operation: () => Promise<void>) => {
        setBusy(true);
        try {
            await operation();
        } catch {
            // The owning page presents the operation error.
        } finally {
            setBusy(false);
        }
    };
    const changeReaction = async (reaction: string, reacted: boolean) => {
        const previous = reactions;
        setReactions(changedReactions(previous, reaction, reacted, emojis));
        try {
            await onReact(displayedNote.id, reaction, reacted);
        } catch (error) {
            setReactions(previous);
            throw error;
        }
    };
    return (
        <NoteSurface>
            {showReplyContext && displayedNote.reply && <ThreadNote label="返信先のノート" lineAfter note={displayedNote.reply} onOpenNote={onOpenNote} onOpenProfile={onOpenProfile} style={{ gridColumn: "1 / -1", marginTop: "-0.5rem", padding: 0, paddingBottom: "0.5rem" }} />}
            {pinned && (
                <div className={rules.pinned} style={styles.pinned}>
                    <IconPinFilled />
                    <span>ピン留めされたノート</span>
                </div>
            )}
            {isRenote && (
                <div className={rules.renoteAttribution} style={styles.renoteAttribution}>
                    <Avatar actor={renoter} onOpenProfile={onOpenProfile} size="xsmall" />
                    <IconRepeat />
                    <button disabled={!renoter} onClick={() => renoter && onOpenProfile(renoter.id)} style={styles.renoteActor} type="button">
                        <EmojiText emojis={renoter?.emojis} text={renoter?.name || renoter?.username || "Unknown"} />
                        さんがリノート
                    </button>
                    <time dateTime={note.created_at} style={styles.renoteTime}>
                        {relativeTime(note.created_at)}
                    </time>
                </div>
            )}
            {renoteUnavailable ? (
                <div style={styles.deleted}>削除されたノート</div>
            ) : (
                <>
                    <Avatar actor={author} onOpenProfile={onOpenProfile} />
                    <div style={styles.body}>
                        <header style={styles.header}>
                            <button disabled={!author} onClick={() => author && onOpenProfile(author.id)} style={styles.actorLink} type="button">
                                <strong style={styles.actorName}>
                                    <EmojiText emojis={author?.emojis} text={author?.name || author?.username || "Unknown"} />
                                </strong>
                                <span>{isRenote ? actorHandle(author) : `@${author?.username || "unknown"}`}</span>
                            </button>
                            {onOpenNote ? (
                                <button aria-label={`ノートの詳細を開く: ${relativeTime(displayedNote.created_at)}`} className={rules.timestamp} onClick={() => onOpenNote(displayedNote.id)} style={styles.timestamp} title={new Date(displayedNote.created_at).toLocaleString("ja")} type="button">
                                    <time dateTime={displayedNote.created_at}>{relativeTime(displayedNote.created_at)}</time>
                                </button>
                            ) : (
                                <time dateTime={displayedNote.created_at} style={styles.timestamp} title={new Date(displayedNote.created_at).toLocaleString("ja")}>
                                    {relativeTime(displayedNote.created_at)}
                                </time>
                            )}
                            <span aria-label={`公開範囲: ${visibility.label}`} className={rules.visibility} role="img" style={styles.visibility} title={visibility.label}>
                                <VisibilityIcon aria-hidden="true" />
                            </span>
                        </header>
                        {displayedNote.content_warning && (
                            <div style={styles.warning}>
                                <strong>{displayedNote.content_warning}</strong>
                                <Button onClick={() => setRevealed((value) => !value)} style={styles.warningButton} variant="secondary">
                                    {revealed ? "隠す" : "表示"}
                                </Button>
                            </div>
                        )}
                        {revealed && displayedNote.text && <Mfm emojis={displayedNote.emojis} nyaize={displayedNote.author?.is_cat} style={styles.text} text={displayedNote.text} />}
                        {revealed && displayedNote.attachments.length > 0 && <NoteMedia attachments={displayedNote.attachments} onOpenImage={setViewerIndex} />}
                        {displayedNote.quote && <QuotedNoteCard onOpen={onOpenNote} onOpenProfile={onOpenProfile} quote={displayedNote.quote} />}
                        {displayedNote.poll && (
                            <div style={styles.poll}>
                                {displayedNote.poll.choices.map((choice) => (
                                    <button aria-pressed={choice.voted} className={rules.pollButton} disabled={busy || displayedNote.poll?.expired} key={choice.index} onClick={() => act(() => onVote(displayedNote.id, choice.index))} style={styles.pollButton} type="button">
                                        <span style={styles.pollText}>{choice.text}</span>
                                        <span style={styles.pollVotes}>{choice.votes}票</span>
                                        <i style={{ ...styles.pollBar, width: `${Math.max(3, choice.votes ? (choice.votes / maxPollVotes) * 100 : 3)}%` }} />
                                    </button>
                                ))}
                            </div>
                        )}
                        <footer style={styles.actions}>
                            <button aria-label={displayedNote.replies_count > 0 ? `返信（${displayedNote.replies_count}件）` : "返信"} className={rules.action} onClick={() => onReply(displayedNote)} style={{ ...styles.action, ...styles.replyAction }} type="button">
                                <IconMessageCircle />
                                {displayedNote.replies_count > 0 && <b style={styles.reactionCount}>{displayedNote.replies_count}</b>}
                            </button>
                            <button aria-label="リノート" className={rules.action} disabled={busy} onClick={() => void act(() => onRenote(displayedNote))} style={styles.action} type="button">
                                <IconRepeat />
                            </button>
                            <button aria-label="引用" className={rules.action} onClick={() => onQuote(displayedNote)} style={styles.action} type="button">
                                <IconQuote />
                            </button>
                            {reactions.map((reaction) => (
                                <button
                                    aria-pressed={reaction.reacted}
                                    className={rules.action}
                                    disabled={busy}
                                    key={reaction.reaction}
                                    onClick={() => act(() => changeReaction(reaction.reaction, reaction.reacted))}
                                    style={{ ...styles.action, ...styles.reaction, ...(reaction.reacted ? styles.reactionActive : {}) }}
                                    type="button"
                                >
                                    {reaction.emoji ? <CustomEmoji emoji={reaction.emoji} label={reaction.reaction} normal /> : <span>{reaction.reaction}</span>}
                                    <b style={styles.reactionCount}>{reaction.count}</b>
                                </button>
                            ))}
                            <button aria-label="リアクションを追加" className={rules.action} disabled={busy} onClick={() => setPickerOpen((value) => !value)} style={styles.action} type="button">
                                ＋
                            </button>
                            {note.author?.id === ownActorID && (
                                <button aria-label="削除" className={`${rules.action} ${rules.deleteAction}`} disabled={busy} onClick={() => setDeleteDialogOpen(true)} style={{ ...styles.action, ...styles.deleteAction }} type="button">
                                    <IconTrash />
                                </button>
                            )}
                        </footer>
                        {pickerOpen && (
                            <div style={styles.picker}>
                                <button className={rules.pickerButton} onClick={() => void act(() => changeReaction("👍", false)).then(() => setPickerOpen(false))} style={styles.pickerButton} type="button">
                                    👍
                                </button>
                                <button className={rules.pickerButton} onClick={() => void act(() => changeReaction("❤️", false)).then(() => setPickerOpen(false))} style={styles.pickerButton} type="button">
                                    ❤️
                                </button>
                                <button className={rules.pickerButton} onClick={() => void act(() => changeReaction("😂", false)).then(() => setPickerOpen(false))} style={styles.pickerButton} type="button">
                                    😂
                                </button>
                                {emojis.map((emoji) => (
                                    <button aria-label={`:${emoji.name}:`} className={rules.pickerButton} key={emoji.name} onClick={() => void act(() => changeReaction(`:${emoji.name}:`, false)).then(() => setPickerOpen(false))} style={styles.pickerButton} title={`:${emoji.name}:`} type="button">
                                        <img alt="" src={emoji.url} style={styles.pickerImage} />
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                </>
            )}
            {viewerIndex !== undefined && <ImageViewer images={imageAttachments} initialIndex={viewerIndex} onClose={() => setViewerIndex(undefined)} />}
            {deleteDialogOpen && (
                <ConfirmDialog
                    busy={busy}
                    confirmLabel="削除する"
                    onCancel={() => setDeleteDialogOpen(false)}
                    onConfirm={() => {
                        setDeleteDialogOpen(false);
                        void act(() => onDelete(note.id));
                    }}
                    title="ノートを削除しますか？"
                >
                    この操作は取り消せません。
                </ConfirmDialog>
            )}
        </NoteSurface>
    );
}
