import { type ChangeEvent, type FormEvent, useEffect, useRef, useState } from "react";

import { IconAlertTriangle, IconChartBar, IconPhoto, IconPlus, IconSend, IconX } from "@tabler/icons-react";

import { api, type CreatePostInput } from "../lib/api";
import { type CanvasThumbnail, createCanvasThumbnail, revokeCanvasThumbnail } from "../lib/image";
import type { Actor, ActorSettings, Emoji, Note } from "../lib/schema";
import { Button, ErrorBanner, Modal } from "./ui";

export type ComposerIntent = { kind: "post" } | { kind: "reply" | "quote"; target: Note };

type PendingImage = { file: File; id: string; intentKey: string; thumbnail: CanvasThumbnail };

export function Composer({ actor, actorSettings, csrf, intent, onClose, onSubmit }: { actor: Actor; actorSettings?: ActorSettings; csrf: string; intent: ComposerIntent; onClose: () => void; onSubmit: (input: CreatePostInput, intentKey: string, noteID: string) => Promise<void> }) {
    const [text, setText] = useState("");
    const [visibility, setVisibility] = useState(actorSettings?.default_visibility || "public");
    const [useCW, setUseCW] = useState(false);
    const [cw, setCW] = useState("");
    const [usePoll, setUsePoll] = useState(false);
    const [choices, setChoices] = useState(() => [
        { id: crypto.randomUUID(), text: "" },
        { id: crypto.randomUUID(), text: "" },
    ]);
    const [multiple, setMultiple] = useState(false);
    const [images, setImages] = useState<PendingImage[]>([]);
    const [sensitive, setSensitive] = useState(false);
    const [emojis, setEmojis] = useState<Emoji[]>([]);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const postIntentKey = useRef<string>(crypto.randomUUID());
    const noteID = useRef<string>(crypto.randomUUID());
    const imagesRef = useRef(images);
    imagesRef.current = images;
    useEffect(() => {
        void api
            .emojis()
            .then(setEmojis)
            .catch(() => setEmojis([]));
    }, []);
    useEffect(
        () => () =>
            imagesRef.current.forEach((image) => {
                revokeCanvasThumbnail(image.thumbnail);
            }),
        [],
    );
    const validChoices = choices.map((choice) => choice.text.trim()).filter(Boolean);
    const canSubmit = text.trim().length > 0 || images.length > 0 || (usePoll && validChoices.length >= 2);
    const selectImages = async (event: ChangeEvent<HTMLInputElement>) => {
        const files = Array.from(event.target.files ?? []).slice(0, 4 - images.length);
        event.target.value = "";
        try {
            const pending = await Promise.all(files.map(async (file) => ({ file, id: crypto.randomUUID(), intentKey: crypto.randomUUID(), thumbnail: await createCanvasThumbnail(file) })));
            setImages((current) => [...current, ...pending]);
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "画像のプレビューを作成できませんでした");
        }
    };
    const submit = async (event: FormEvent) => {
        event.preventDefault();
        if (!canSubmit) return;
        setBusy(true);
        setError("");
        try {
            const uploaded = await Promise.all(images.map((image) => api.uploadImage(csrf, actor.id, image.file, image.thumbnail, image.intentKey)));
            await onSubmit(
                {
                    text: text.trim(),
                    visibility,
                    ...(useCW && cw.trim() ? { content_warning: cw.trim() } : {}),
                    ...(intent.kind === "reply" ? { in_reply_to_uri: intent.target.uri } : {}),
                    ...(intent.kind === "quote" ? { quote_uri: intent.target.uri } : {}),
                    ...(usePoll ? { poll: { choices: validChoices, multiple } } : {}),
                    ...(uploaded.length > 0 ? { media_ids: uploaded.map((item) => item.id), sensitive } : {}),
                    emoji_names: emojis.filter((emoji) => text.includes(`:${emoji.name}:`)).map((emoji) => emoji.name),
                },
                postIntentKey.current,
                noteID.current,
            );
            onClose();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "投稿できませんでした");
        } finally {
            setBusy(false);
        }
    };
    const title = intent.kind === "reply" ? "返信する" : intent.kind === "quote" ? "引用する" : "新しいノート";
    return (
        <Modal label={title} onClose={onClose}>
            <form className="composer" onSubmit={submit}>
                <header>
                    <p className="eyebrow">@{actor.username}として</p>
                    <h2>{title}</h2>
                </header>
                {intent.kind !== "post" && (
                    <p className="composer__target">
                        {intent.target.author?.name || intent.target.author?.username}: {intent.target.text || "（本文なし）"}
                    </p>
                )}
                {error && <ErrorBanner message={error} />}
                <textarea aria-label="ノート本文" autoFocus maxLength={3000} onChange={(event) => setText(event.target.value)} placeholder="いまどうしてる？" rows={7} value={text} />
                {images.length > 0 && (
                    <div className="upload-previews">
                        {images.map((image) => (
                            <figure key={image.id}>
                                <img alt={image.file.name} src={image.thumbnail.url} />
                                <button
                                    aria-label={`${image.file.name}を削除`}
                                    onClick={() => {
                                        revokeCanvasThumbnail(image.thumbnail);
                                        setImages((current) => current.filter((item) => item.id !== image.id));
                                    }}
                                    type="button"
                                >
                                    <IconX />
                                </button>
                                <figcaption>
                                    {image.thumbnail.originalWidth} × {image.thumbnail.originalHeight}
                                </figcaption>
                            </figure>
                        ))}
                    </div>
                )}
                {emojis.length > 0 && (
                    <div aria-label="カスタム絵文字" className="emoji-picker">
                        {emojis.map((emoji) => (
                            <button aria-label={`:${emoji.name}:`} key={emoji.name} onClick={() => setText((value) => `${value}:${emoji.name}:`)} title={`:${emoji.name}:`} type="button">
                                <img alt="" src={emoji.url} />
                            </button>
                        ))}
                    </div>
                )}
                {useCW && <input aria-label="内容の注記" className="composer__cw" maxLength={500} onChange={(event) => setCW(event.target.value)} placeholder="内容の注記" value={cw} />}
                {usePoll && (
                    <fieldset className="poll-editor">
                        <legend>アンケート</legend>
                        {choices.map((choice, index) => (
                            <div key={choice.id}>
                                <input aria-label={`選択肢 ${index + 1}`} maxLength={200} onChange={(event) => setChoices((current) => current.map((value) => (value.id === choice.id ? { ...value, text: event.target.value } : value)))} placeholder={`選択肢 ${index + 1}`} value={choice.text} />
                                {choices.length > 2 && (
                                    <button aria-label={`選択肢 ${index + 1}を削除`} onClick={() => setChoices((current) => current.filter((value) => value.id !== choice.id))} type="button">
                                        <IconX />
                                    </button>
                                )}
                            </div>
                        ))}
                        {choices.length < 10 && (
                            <button className="poll-editor__add" onClick={() => setChoices((current) => [...current, { id: crypto.randomUUID(), text: "" }])} type="button">
                                <IconPlus />
                                選択肢を追加
                            </button>
                        )}
                        <label>
                            <input checked={multiple} onChange={(event) => setMultiple(event.target.checked)} type="checkbox" />
                            複数回答を許可
                        </label>
                    </fieldset>
                )}
                <footer>
                    <div className="composer__options">
                        <label>
                            <span>公開範囲</span>
                            <select onChange={(event) => setVisibility(event.target.value as typeof visibility)} value={visibility}>
                                <option value="public">公開</option>
                                <option value="home">ホーム</option>
                                <option value="followers">フォロワー</option>
                            </select>
                        </label>
                        <button aria-pressed={useCW} className={useCW ? "icon-toggle icon-toggle--active" : "icon-toggle"} onClick={() => setUseCW((value) => !value)} type="button">
                            <IconAlertTriangle />
                            CW
                        </button>
                        <button aria-pressed={usePoll} className={usePoll ? "icon-toggle icon-toggle--active" : "icon-toggle"} disabled={intent.kind !== "post"} onClick={() => setUsePoll((value) => !value)} type="button">
                            <IconChartBar />
                            投票
                        </button>
                        <label className="icon-toggle upload-button">
                            <IconPhoto />
                            画像
                            <input accept="image/jpeg,image/png,image/gif,image/webp" disabled={images.length >= 4} multiple onChange={(event) => void selectImages(event)} type="file" />
                        </label>
                    </div>
                    {images.length > 0 && (
                        <label className="sensitive-toggle">
                            <input checked={sensitive} onChange={(event) => setSensitive(event.target.checked)} type="checkbox" />
                            センシティブ
                        </label>
                    )}
                    <Button disabled={busy || !canSubmit} type="submit">
                        <IconSend />
                        投稿する
                    </Button>
                </footer>
            </form>
        </Modal>
    );
}
