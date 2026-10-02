import { type FormEvent, useEffect, useRef, useState } from "react";

import { IconCamera, IconUserCircle } from "@tabler/icons-react";

import { EmojiText } from "@/components/EmojiText";
import { ImageFileInput } from "@/components/ImageFileInput";
import { Mfm } from "@/components/Mfm";
import { Button, ErrorBanner, Modal } from "@/components/ui";
import { api } from "@/lib/api";
import { css } from "@/lib/css";
import { type CropOptions, processProfileImage } from "@/lib/profileImage";
import type { Actor } from "@/lib/schema";
import { type UploadedImage, uploadImage } from "@/lib/uploader";

type Target = "avatar" | "banner";
type DraftImage = { file: File; width: number; height: number; url: string; uploaded?: UploadedImage };
type Selection = DraftImage & { target: Target; cropping: boolean };
const field = css({ display: "grid", gap: ".375rem", "& input, & textarea": { width: "100%", padding: ".75rem", border: "1px solid var(--border)", borderRadius: "1rem", color: "var(--text)", background: "var(--panel-muted)" } });
const bannerStyle = { width: "100%", height: "13rem", background: "radial-gradient(circle at 18% 25%, var(--accent), transparent 32%), linear-gradient(135deg, var(--accent-soft), var(--panel-muted))" };

export function ProfileSettings({ actor, csrf, onActorsChanged }: { actor: Actor; csrf: string; onActorsChanged: () => Promise<void> }) {
    const [name, setName] = useState(actor.name);
    const [summary, setSummary] = useState(actor.summary);
    const [images, setImages] = useState<Partial<Record<Target, DraftImage>>>({});
    const [selection, setSelection] = useState<Selection>();
    const [crop, setCrop] = useState<CropOptions>({ aspect: 1, zoom: 1, x: 0.5, y: 0.5 });
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");
    const lifecycle = useRef<AbortController | null>(null);
    const urls = useRef(new Set<string>());
    const working = useRef(false);
    useEffect(() => {
        const controller = new AbortController();
        lifecycle.current = controller;
        return () => {
            controller.abort();
            for (const url of urls.current) URL.revokeObjectURL(url);
            urls.current.clear();
        };
    }, []);
    const previewURL = (blob: Blob) => {
        const url = URL.createObjectURL(blob);
        urls.current.add(url);
        return url;
    };
    const release = (url: string) => {
        URL.revokeObjectURL(url);
        urls.current.delete(url);
    };
    const select = async (target: Target, file?: File) => {
        if (!file || working.current || !lifecycle.current) return;
        working.current = true;
        setBusy(true);
        setError("");
        setMessage("");
        try {
            if (!["image/jpeg", "image/png", "image/gif", "image/webp"].includes(file.type)) throw new Error("JPEG、PNG、GIF、WebPの画像を選択してください");
            if (file.size > 32 * 1024 * 1024) throw new Error("32MB以下の画像を選択してください");
            const result = await processProfileImage(file, lifecycle.current.signal);
            if (lifecycle.current.signal.aborted) return;
            setCrop({ aspect: target === "avatar" ? 1 : 3, zoom: 1, x: 0.5, y: 0.5 });
            setSelection({ file, target, width: result.width, height: result.height, url: previewURL(result.blob), cropping: false });
        } catch (reason) {
            if (!lifecycle.current.signal.aborted) setError(reason instanceof Error ? reason.message : "画像を読み込めませんでした");
        } finally {
            working.current = false;
            if (!lifecycle.current.signal.aborted) setBusy(false);
        }
    };
    const accept = async (cropping: boolean) => {
        if (!selection || working.current || !lifecycle.current) return;
        working.current = true;
        setBusy(true);
        setError("");
        try {
            let draft: DraftImage = selection;
            if (cropping) {
                const result = await processProfileImage(selection.file, lifecycle.current.signal, crop);
                if (lifecycle.current.signal.aborted) return;
                draft = { file: new File([result.blob], `${selection.target}.${result.blob.type === "image/webp" ? "webp" : "jpg"}`, { type: result.blob.type }), width: result.width, height: result.height, url: previewURL(result.blob) };
                release(selection.url);
            }
            const previous = images[selection.target];
            if (previous) release(previous.url);
            setImages((current) => ({ ...current, [selection.target]: draft }));
            setSelection(undefined);
        } catch (reason) {
            if (!lifecycle.current.signal.aborted) setError(reason instanceof Error ? reason.message : "クロップできませんでした");
        } finally {
            working.current = false;
            if (!lifecycle.current.signal.aborted) setBusy(false);
        }
    };
    const save = async (event: FormEvent) => {
        event.preventDefault();
        if (working.current || selection || !lifecycle.current) return;
        working.current = true;
        setBusy(true);
        setError("");
        setMessage("");
        try {
            const patch: Record<string, unknown> = { name, summary };
            for (const target of ["avatar", "banner"] as const) {
                const draft = images[target];
                if (!draft) continue;
                // Retain completed uploads across retries. A queued PATCH may have been accepted even if its response was lost.
                draft.uploaded ??= await uploadImage(csrf, actor.id, draft.file, draft);
                if (lifecycle.current.signal.aborted) return;
                if (!draft.uploaded.source_url) throw new Error("アップロード元URLを取得できませんでした");
                patch[`${target}_url`] = draft.uploaded.source_url;
            }
            await api.updateActor(csrf, actor.id, patch);
            if (lifecycle.current.signal.aborted) return;
            await onActorsChanged();
            if (!lifecycle.current.signal.aborted) setMessage("プロフィールを保存しました");
        } catch (reason) {
            if (!lifecycle.current.signal.aborted) setError(reason instanceof Error ? reason.message : "プロフィールを保存できませんでした");
        } finally {
            working.current = false;
            if (!lifecycle.current.signal.aborted) setBusy(false);
        }
    };
    const closeSelection = () => {
        if (busy || !selection) return;
        release(selection.url);
        setSelection(undefined);
    };
    const preview = { ...actor, name, summary, avatar_url: images.avatar?.url || actor.avatar_url, banner_url: images.banner?.url || actor.banner_url };
    const ratio = selection ? selection.width / selection.height : 1;
    const relativeWidth = Math.max(1, ratio / crop.aspect) * crop.zoom;
    const relativeHeight = Math.max(1, crop.aspect / ratio) * crop.zoom;
    return (
        <section style={{ border: "1px solid var(--border)", borderRadius: "1.5rem", overflow: "hidden", background: "var(--panel)" }}>
            <h2 style={{ padding: "1rem" }}>プロフィールを編集</h2>
            <section aria-label="プロフィールプレビュー">
                <div style={{ ...bannerStyle, overflow: "hidden" }}>{preview.banner_url && <img alt="バナーのプレビュー" src={preview.banner_url} style={{ width: "100%", height: "100%", objectFit: "cover" }} />}</div>
                <div style={{ padding: "0 1.5rem 1.5rem" }}>
                    <div style={{ width: "6rem", height: "6rem", marginTop: "-3rem", position: "relative", border: "4px solid var(--panel)", borderRadius: "50%", overflow: "hidden", background: "var(--panel-muted)" }}>
                        {preview.avatar_url ? <img alt="アバターのプレビュー" src={preview.avatar_url} style={{ width: "100%", height: "100%", objectFit: "cover" }} /> : <IconUserCircle size="100%" />}
                    </div>
                    <h3>
                        <EmojiText emojis={actor.emojis ?? []} text={name || actor.username} />
                    </h3>
                    <p style={{ color: "var(--muted)" }}>@{actor.username}</p>
                    {summary && <Mfm emojis={actor.emojis ?? []} text={summary} />}
                </div>
            </section>
            <form onSubmit={save} style={{ padding: "1.5rem", display: "grid", gap: "1rem" }}>
                {error && <ErrorBanner message={error} />}
                {message && <p role="status">{message}</p>}
                <fieldset disabled={busy || !!selection} style={{ border: 0, padding: 0, display: "grid", gap: "1rem", minWidth: 0 }}>
                    {(["avatar", "banner"] as const).map((target) => (
                        <label className={field} key={target}>
                            <span>
                                <IconCamera size={18} /> {target === "avatar" ? "アバター画像" : "バナー画像"}
                            </span>
                            <ImageFileInput onSelect={(files) => void select(target, files[0])} />
                        </label>
                    ))}
                    <label className={field}>
                        表示名
                        <input maxLength={128} value={name} onChange={(event) => setName(event.target.value)} />
                    </label>
                    <label className={field}>
                        自己紹介
                        <textarea rows={4} maxLength={1500} value={summary} onChange={(event) => setSummary(event.target.value)} />
                    </label>
                </fieldset>
                <Button disabled={busy || !!selection} type="submit">
                    {busy ? "処理中…" : "プロフィールを保存"}
                </Button>
            </form>
            {selection && (
                <Modal label={selection.cropping ? "画像をクロップ" : "画像をクロップしますか？"} onClose={closeSelection}>
                    <h2>{selection.cropping ? "画像をクロップ" : "画像をクロップしますか？"}</h2>
                    {error && <ErrorBanner message={error} />}
                    {selection.cropping ? (
                        <>
                            <p>位置と拡大率を調整してください。クロップするとアニメーションは静止画になります。</p>
                            <div aria-label="クロッププレビュー" style={{ position: "relative", overflow: "hidden", aspectRatio: crop.aspect, borderRadius: selection.target === "avatar" ? "50%" : ".5rem", background: "var(--panel-muted)" }}>
                                <img alt="切り抜く範囲" src={selection.url} style={{ position: "absolute", maxWidth: "none", width: `${relativeWidth * 100}%`, height: `${relativeHeight * 100}%`, left: `${(1 - relativeWidth) * crop.x * 100}%`, top: `${(1 - relativeHeight) * crop.y * 100}%` }} />
                            </div>
                            <fieldset disabled={busy} style={{ border: 0, padding: 0, display: "grid", gap: "1rem", marginBlock: "1rem" }}>
                                {(
                                    [
                                        { key: "zoom", label: "拡大率", min: 1, max: 4 },
                                        { key: "x", label: "横位置", min: 0, max: 1 },
                                        { key: "y", label: "縦位置", min: 0, max: 1 },
                                    ] as const
                                ).map(({ key, label, min, max }) => (
                                    <label className={field} key={key}>
                                        {label}
                                        <input type="range" min={min} max={max} step={0.01} value={crop[key]} onChange={(event) => setCrop((current) => ({ ...current, [key]: Number(event.target.value) }))} />
                                    </label>
                                ))}
                            </fieldset>
                            <Button disabled={busy} onClick={() => void accept(true)}>
                                クロップを適用
                            </Button>
                        </>
                    ) : (
                        <>
                            <p>アバターは正方形、バナーは3:1に切り抜けます。クロップしない場合は元のファイルをアップロードします。</p>
                            <Button onClick={() => setSelection({ ...selection, cropping: true })}>はい、クロップする</Button>
                            <Button variant="secondary" onClick={() => void accept(false)}>
                                いいえ、そのまま使う
                            </Button>
                        </>
                    )}
                    <Button disabled={busy} variant="ghost" onClick={closeSelection}>
                        キャンセル
                    </Button>
                </Modal>
            )}
        </section>
    );
}
