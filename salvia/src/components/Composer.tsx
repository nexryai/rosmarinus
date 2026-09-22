import { type CSSProperties, type FormEvent, useEffect, useRef, useState } from "react";

import { IconAlertTriangle, IconChartBar, IconMoodSmile, IconPhoto, IconPlus, IconSend, IconX } from "@tabler/icons-react";

import { api, type CreatePostInput } from "../lib/api";
import { css } from "../lib/css";
import { type CanvasThumbnail, createCanvasThumbnail, revokeCanvasThumbnail } from "../lib/image";
import type { Actor, ActorSettings, Emoji, Note } from "../lib/schema";
import { uploadImage } from "../lib/uploader";
import { EmojiPickerDialog } from "./EmojiPickerDialog";
import { EmojiText } from "./EmojiText";
import { ImageFileInput } from "./ImageFileInput";
import { Mfm } from "./Mfm";
import { Button, ErrorBanner, Modal } from "./ui";
import { Dropdown, type DropdownOption } from "./ui/Dropdown";
import { Switch } from "./ui/Switch";

export type ComposerIntent = { kind: "post" } | { kind: "reply" | "quote"; target: Note };

type PendingImage = { file: File; id: string; intentKey: string; thumbnail: CanvasThumbnail };

const styles = {
    eyebrow: {
        marginBottom: "0.125rem",
        color: "var(--muted)",
        fontSize: "11px",
        fontWeight: 700,
        letterSpacing: "0.1em",
        textTransform: "uppercase",
    },
    title: {
        fontSize: "1.25rem",
        lineHeight: 1.4,
        fontWeight: 900,
    },
    target: {
        marginTop: "0.75rem",
        padding: "0.75rem",
        display: "-webkit-box",
        overflow: "hidden",
        WebkitBoxOrient: "vertical",
        WebkitLineClamp: 3,
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        color: "var(--muted)",
        background: "var(--panel-muted)",
        fontSize: "0.875rem",
    },
    textarea: {
        width: "100%",
        marginTop: "1.25rem",
        padding: 0,
        resize: "vertical",
        border: 0,
        outline: "none",
        color: "var(--text)",
        background: "transparent",
        fontSize: "1.125rem",
        lineHeight: "1.75rem",
    },
    previews: {
        marginTop: "0.75rem",
        display: "grid",
        gridTemplateColumns: "repeat(2, minmax(0, 1fr))",
        gap: "0.5rem",
    },
    preview: {
        position: "relative",
        overflow: "hidden",
        borderRadius: "1rem",
        background: "var(--panel-muted)",
    },
    previewImage: {
        width: "100%",
        height: "100%",
        aspectRatio: "16 / 9",
        objectFit: "cover",
    },
    previewRemove: {
        position: "absolute",
        top: "0.5rem",
        right: "0.5rem",
        width: "2rem",
        height: "2rem",
        display: "grid",
        placeItems: "center",
        borderRadius: "9999px",
        color: "#fff",
        backgroundColor: "rgb(0 0 0 / 65%)",
    },
    previewCaption: {
        position: "absolute",
        right: "0.5rem",
        bottom: "0.25rem",
        padding: "0.125rem 0.375rem",
        borderRadius: "0.25rem",
        color: "#fff",
        backgroundColor: "rgb(0 0 0 / 55%)",
        fontSize: "10px",
    },
    input: {
        width: "100%",
        marginTop: "0.75rem",
        padding: "0.625rem 1rem",
        borderWidth: 1,
        borderStyle: "solid",
        borderRadius: "1rem",
        outline: "none",
        color: "var(--text)",
        transition: "border-color 150ms, background-color 150ms",
    },
    poll: {
        marginTop: "0.75rem",
        padding: "0.75rem",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
    },
    pollLegend: {
        paddingInline: "0.25rem",
        fontSize: "0.875rem",
        fontWeight: 700,
    },
    choiceRow: {
        display: "flex",
        gap: "0.5rem",
        marginBottom: "0.5rem",
    },
    choiceInput: {
        minWidth: 0,
        padding: "0.5rem 0.75rem",
        border: "1px solid var(--border)",
        borderRadius: "0.75rem",
        color: "var(--text)",
        background: "var(--panel-muted)",
        flex: 1,
    },
    choiceRemove: {
        width: "2.5rem",
        height: "2.5rem",
        display: "grid",
        placeItems: "center",
        borderRadius: "9999px",
        color: "var(--muted)",
    },
    addChoice: {
        display: "flex",
        alignItems: "center",
        gap: "0.25rem",
        color: "var(--accent-hover)",
        fontSize: "0.875rem",
        fontWeight: 700,
    },
    pollToggle: {
        marginTop: "0.5rem",
        display: "flex",
        alignItems: "center",
        gap: "0.5rem",
        fontSize: "0.875rem",
    },
    footer: {
        marginTop: "1.25rem",
        paddingTop: "1rem",
        alignItems: "flex-end",
        gap: "0.75rem",
        borderTop: "1px solid var(--border)",
    },
    options: {
        display: "flex",
        alignItems: "flex-end",
        gap: "0.5rem",
        flex: 1,
    },
    optionLabel: {
        display: "block",
        color: "var(--muted)",
        fontSize: "10px",
        fontWeight: 700,
        textTransform: "uppercase",
    },
    visibility: {
        minWidth: "8.5rem",
    },
    visibilityTrigger: {
        minHeight: "2.25rem",
        paddingBlock: "0.375rem",
        fontSize: "0.8rem",
        fontWeight: 700,
    },
    iconToggle: {
        minHeight: "2.25rem",
        paddingInline: "0.75rem",
        display: "flex",
        alignItems: "center",
        gap: "0.25rem",
        border: "1px solid var(--border)",
        borderRadius: "9999px",
        color: "var(--muted)",
    },
    iconToggleActive: {
        color: "var(--accent-ink)",
        borderColor: "var(--accent)",
        background: "var(--accent-soft)",
    },
    upload: {
        position: "relative",
        cursor: "pointer",
    },
    srOnly: {
        position: "absolute",
        width: 1,
        height: 1,
        margin: -1,
        padding: 0,
        overflow: "hidden",
        clipPath: "inset(50%)",
        whiteSpace: "nowrap",
    },
    sensitive: {
        display: "flex",
        alignItems: "center",
        gap: "0.25rem",
        color: "var(--muted)",
        fontSize: "0.75rem",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    input: css({
        borderColor: "var(--border)",
        background: "var(--panel-muted)",
        "&:focus": {
            borderColor: "var(--accent-hover)",
            background: "var(--panel)",
        },
    }),
    previewRemove: css({
        "& svg": {
            width: "1rem",
            height: "1rem",
        },
    }),
    choiceIcon: css({
        "& svg": {
            width: "1rem",
            height: "1rem",
        },
    }),
    footer: css({
        display: "flex",
        "@media (width <= 639px)": {
            flexDirection: "column",
            alignItems: "stretch",
        },
    }),
    iconToggle: css({
        position: "relative",
        "& svg": {
            width: "1rem",
            height: "1rem",
        },
        "&::after": {
            content: "attr(data-label)",
            position: "absolute",
            zIndex: 10,
            bottom: "calc(100% + 0.5rem)",
            left: "50%",
            padding: "0.25rem 0.5rem",
            borderRadius: "0.5rem",
            color: "var(--panel)",
            background: "var(--text)",
            boxShadow: "0 6px 18px rgb(0 0 0 / 18%)",
            fontSize: "0.7rem",
            fontWeight: 700,
            lineHeight: 1.2,
            whiteSpace: "nowrap",
            opacity: 0,
            transform: "translateX(-50%) translateY(0.25rem)",
            transition: "opacity 120ms ease, transform 120ms ease",
            pointerEvents: "none",
        },
        "&:hover::after, &:focus-visible::after": {
            opacity: 1,
            transform: "translateX(-50%) translateY(0)",
        },
        "@media (prefers-reduced-motion: reduce)": {
            "&::after": {
                transitionDuration: "0.01ms",
            },
        },
    }),
    upload: css({
        "&:focus-within": {
            outline: "3px solid var(--accent)",
            outlineOffset: "2px",
        },
        "&:focus-within::after": {
            opacity: 1,
            transform: "translateX(-50%) translateY(0)",
        },
    }),
};

type Visibility = "public" | "home" | "followers";

const visibilityOptions = [
    { value: "public", label: "公開", description: "すべてのユーザーに公開" },
    { value: "home", label: "ホーム", description: "連合タイムラインに表示しない" },
    { value: "followers", label: "フォロワー", description: "フォロワーだけに公開" },
] satisfies DropdownOption<Visibility>[];

export function Composer({ actor, actorSettings, csrf, intent, onClose, onSubmit }: { actor: Actor; actorSettings?: ActorSettings; csrf: string; intent: ComposerIntent; onClose: () => void; onSubmit: (input: CreatePostInput, intentKey: string) => Promise<void> }) {
    const [text, setText] = useState("");
    const [visibility, setVisibility] = useState<Visibility>(actorSettings?.default_visibility || "public");
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
    const [emojiPickerOpen, setEmojiPickerOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const postIntentKey = useRef<string>(crypto.randomUUID());
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
    const selectImages = async (files: File[]) => {
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
        let uploaded: Awaited<ReturnType<typeof uploadImage>>[] = [];
        let committed = false;
        try {
            const results = await Promise.allSettled(images.map((image) => uploadImage(csrf, actor.id, image.file, { width: image.thumbnail.originalWidth, height: image.thumbnail.originalHeight }, image.intentKey)));
            uploaded = results.flatMap((result) => (result.status === "fulfilled" ? [result.value] : []));
            const failed = results.find((result) => result.status === "rejected");
            if (failed?.status === "rejected") throw failed.reason;
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
            );
            committed = true;
            onClose();
        } catch (reason) {
            if (!committed) await Promise.all(uploaded.map((item) => api.deleteMedia(csrf, actor.id, item.id).catch(() => undefined)));
            setError(reason instanceof Error ? reason.message : "投稿できませんでした");
        } finally {
            setBusy(false);
        }
    };
    const title = intent.kind === "reply" ? "返信する" : intent.kind === "quote" ? "引用する" : "新しいノート";
    return (
        <>
            <Modal dismissible={!emojiPickerOpen} label={title} onClose={onClose}>
                <form onSubmit={submit}>
                    <header>
                        <p style={styles.eyebrow}>@{actor.username}として</p>
                        <h2 style={styles.title}>{title}</h2>
                    </header>
                    {intent.kind !== "post" && (
                        <p style={styles.target}>
                            <EmojiText emojis={intent.target.author?.emojis} text={intent.target.author?.name || intent.target.author?.username || "Unknown"} />: <Mfm emojis={intent.target.emojis} nyaize={intent.target.author?.is_cat} text={intent.target.text || "（本文なし）"} />
                        </p>
                    )}
                    {error && <ErrorBanner message={error} />}
                    <textarea aria-label="ノート本文" autoFocus maxLength={3000} onChange={(event) => setText(event.target.value)} placeholder="いまどうしてる？" rows={7} style={styles.textarea} value={text} />
                    {images.length > 0 && (
                        <div style={styles.previews}>
                            {images.map((image) => (
                                <figure key={image.id} style={styles.preview}>
                                    <img alt={image.file.name} src={image.thumbnail.url} style={styles.previewImage} />
                                    <button
                                        aria-label={`${image.file.name}を削除`}
                                        onClick={() => {
                                            revokeCanvasThumbnail(image.thumbnail);
                                            setImages((current) => current.filter((item) => item.id !== image.id));
                                        }}
                                        className={rules.previewRemove}
                                        style={styles.previewRemove}
                                        type="button"
                                    >
                                        <IconX />
                                    </button>
                                    <figcaption style={styles.previewCaption}>
                                        {image.thumbnail.originalWidth} × {image.thumbnail.originalHeight}
                                    </figcaption>
                                </figure>
                            ))}
                        </div>
                    )}
                    {useCW && <input aria-label="内容の注記" className={rules.input} maxLength={500} onChange={(event) => setCW(event.target.value)} placeholder="内容の注記" style={styles.input} value={cw} />}
                    {usePoll && (
                        <fieldset style={styles.poll}>
                            <legend style={styles.pollLegend}>アンケート</legend>
                            {choices.map((choice, index) => (
                                <div key={choice.id} style={styles.choiceRow}>
                                    <input
                                        aria-label={`選択肢 ${index + 1}`}
                                        maxLength={200}
                                        onChange={(event) => setChoices((current) => current.map((value) => (value.id === choice.id ? { ...value, text: event.target.value } : value)))}
                                        placeholder={`選択肢 ${index + 1}`}
                                        style={styles.choiceInput}
                                        value={choice.text}
                                    />
                                    {choices.length > 2 && (
                                        <button aria-label={`選択肢 ${index + 1}を削除`} className={rules.choiceIcon} onClick={() => setChoices((current) => current.filter((value) => value.id !== choice.id))} style={styles.choiceRemove} type="button">
                                            <IconX />
                                        </button>
                                    )}
                                </div>
                            ))}
                            {choices.length < 10 && (
                                <button className={rules.choiceIcon} onClick={() => setChoices((current) => [...current, { id: crypto.randomUUID(), text: "" }])} style={styles.addChoice} type="button">
                                    <IconPlus />
                                    選択肢を追加
                                </button>
                            )}
                            <Switch checked={multiple} label="複数回答を許可" onChange={setMultiple} style={styles.pollToggle} />
                        </fieldset>
                    )}
                    <footer className={rules.footer} style={styles.footer}>
                        <div style={styles.options}>
                            <div>
                                <span style={styles.optionLabel}>公開範囲</span>
                                <Dropdown label="公開範囲" onChange={setVisibility} options={visibilityOptions} placement="top" style={styles.visibility} triggerStyle={styles.visibilityTrigger} value={visibility} />
                            </div>
                            <button aria-label="CW" aria-pressed={useCW} className={rules.iconToggle} data-label="CW" onClick={() => setUseCW((value) => !value)} style={{ ...styles.iconToggle, ...(useCW ? styles.iconToggleActive : {}) }} type="button">
                                <IconAlertTriangle />
                            </button>
                            <button aria-label="投票" aria-pressed={usePoll} className={rules.iconToggle} data-label="投票" disabled={intent.kind !== "post"} onClick={() => setUsePoll((value) => !value)} style={{ ...styles.iconToggle, ...(usePoll ? styles.iconToggleActive : {}) }} type="button">
                                <IconChartBar />
                            </button>
                            <label className={`${rules.iconToggle} ${rules.upload}`} data-label="画像" style={{ ...styles.iconToggle, ...styles.upload }}>
                                <IconPhoto />
                                <span style={styles.srOnly}>画像</span>
                                <ImageFileInput disabled={images.length >= 4} hidden maxFiles={4 - images.length} multiple onSelect={(files) => void selectImages(files)} />
                            </label>
                            <button aria-label="絵文字" className={rules.iconToggle} data-label="絵文字" onClick={() => setEmojiPickerOpen(true)} style={styles.iconToggle} type="button">
                                <IconMoodSmile />
                            </button>
                        </div>
                        {images.length > 0 && <Switch checked={sensitive} label="センシティブ" onChange={setSensitive} style={styles.sensitive} />}
                        <Button disabled={busy || !canSubmit} type="submit">
                            <IconSend />
                            投稿する
                        </Button>
                    </footer>
                </form>
            </Modal>
            {emojiPickerOpen && <EmojiPickerDialog emojis={emojis} label="絵文字を選択" onClose={() => setEmojiPickerOpen(false)} onSelect={(value) => setText((current) => `${current}${value}`)} title="絵文字" />}
        </>
    );
}
