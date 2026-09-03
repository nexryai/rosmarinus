import { useState } from "react";

import { IconMessageCircle, IconQuote, IconRepeat, IconTrash } from "@tabler/icons-react";

import type { Emoji, Note } from "../lib/schema";
import { Avatar, Button } from "./ui";

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
        return emoji ? <img alt={part} className="inline-emoji" key={tokenOffset} loading="lazy" src={emoji.url} /> : part;
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
        <article className="note-card">
            <Avatar actor={author} />
            <div className="note-card__body">
                <header>
                    <button className="actor-link" disabled={!author} onClick={() => author && onOpenProfile(author.id)} type="button">
                        <strong>{author?.name || author?.username || "Unknown"}</strong>
                        <span>@{author?.username || "unknown"}</span>
                    </button>
                    <span>·</span>
                    <time dateTime={note.created_at}>{relativeTime(note.created_at)}</time>
                    <span className="note-visibility">{note.visibility === "followers" ? "フォロワー" : note.visibility === "home" ? "ホーム" : note.visibility === "specified" ? "宛先指定" : "公開"}</span>
                    {onOpenNote && (
                        <button className="note-detail-link" onClick={() => onOpenNote(note.id)} type="button">
                            詳細
                        </button>
                    )}
                </header>
                {note.renote && (
                    <div className="reference">
                        <IconRepeat />
                        <span>{note.renote.author?.name || note.renote.author?.username}</span>
                        <p>{note.renote.text}</p>
                    </div>
                )}
                {note.content_warning && (
                    <div className="content-warning">
                        <strong>{note.content_warning}</strong>
                        <Button onClick={() => setRevealed((value) => !value)} variant="secondary">
                            {revealed ? "隠す" : "表示"}
                        </Button>
                    </div>
                )}
                {revealed && note.text && <p className="note-text">{renderText(note.text, note)}</p>}
                {revealed && note.attachments.length > 0 && (
                    <div className={`attachments attachments--${Math.min(note.attachments.length, 4)}`}>
                        {note.attachments.map((attachment) =>
                            attachment.media_type?.startsWith("image/") ? (
                                <a href={attachment.url} key={attachment.url} rel="noreferrer" target="_blank">
                                    <img alt={attachment.name || "添付画像"} loading="lazy" referrerPolicy="no-referrer" src={attachment.url} />
                                </a>
                            ) : (
                                <a className="attachment-file" href={attachment.url} key={attachment.url} rel="noreferrer" target="_blank">
                                    {attachment.name || "添付ファイル"}
                                </a>
                            ),
                        )}
                    </div>
                )}
                {note.quote && (
                    <div className="quote">
                        <strong>{note.quote.author?.name || note.quote.author?.username}</strong>
                        <p>{note.quote.text}</p>
                    </div>
                )}
                {note.poll && (
                    <div className="poll">
                        {note.poll.choices.map((choice) => (
                            <button aria-pressed={choice.voted} disabled={busy || note.poll?.expired} key={choice.index} onClick={() => act(() => onVote(note.id, choice.index))} type="button">
                                <span>{choice.text}</span>
                                <span>{choice.votes}票</span>
                                <i style={{ width: `${Math.max(3, choice.votes ? (choice.votes / maxPollVotes) * 100 : 3)}%` }} />
                            </button>
                        ))}
                    </div>
                )}
                <footer className="note-actions">
                    <button aria-label="返信" onClick={() => onReply(note)} type="button">
                        <IconMessageCircle />
                    </button>
                    <button aria-label="リノート" disabled={busy} onClick={() => void act(() => onRenote(note))} type="button">
                        <IconRepeat />
                    </button>
                    <button aria-label="引用" onClick={() => onQuote(note)} type="button">
                        <IconQuote />
                    </button>
                    {note.reactions.map((reaction) => (
                        <button aria-pressed={reaction.reacted} className={reaction.reacted ? "reaction reaction--active" : "reaction"} disabled={busy} key={reaction.reaction} onClick={() => act(() => onReact(note.id, reaction.reaction, reaction.reacted))} type="button">
                            <span>{reaction.reaction}</span>
                            <b>{reaction.count}</b>
                        </button>
                    ))}
                    <button aria-label="リアクションを追加" className="reaction-add" disabled={busy} onClick={() => setPickerOpen((value) => !value)} type="button">
                        ＋
                    </button>
                    {author?.id === ownActorID && (
                        <button
                            aria-label="削除"
                            className="delete-note"
                            disabled={busy}
                            onClick={() => {
                                if (window.confirm("このノートを削除しますか？")) void act(() => onDelete(note.id));
                            }}
                            type="button"
                        >
                            <IconTrash />
                        </button>
                    )}
                </footer>
                {pickerOpen && (
                    <div className="reaction-picker">
                        <button onClick={() => void act(() => onReact(note.id, "👍", false)).then(() => setPickerOpen(false))} type="button">
                            👍
                        </button>
                        <button onClick={() => void act(() => onReact(note.id, "❤️", false)).then(() => setPickerOpen(false))} type="button">
                            ❤️
                        </button>
                        <button onClick={() => void act(() => onReact(note.id, "😂", false)).then(() => setPickerOpen(false))} type="button">
                            😂
                        </button>
                        {emojis.map((emoji) => (
                            <button aria-label={`:${emoji.name}:`} key={emoji.name} onClick={() => void act(() => onReact(note.id, `:${emoji.name}:`, false)).then(() => setPickerOpen(false))} title={`:${emoji.name}:`} type="button">
                                <img alt="" src={emoji.url} />
                            </button>
                        ))}
                    </div>
                )}
            </div>
        </article>
    );
}
