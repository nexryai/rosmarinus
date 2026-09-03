import { type FormEvent, useState } from "react";

import { IconAlertTriangle, IconSend } from "@tabler/icons-react";

import type { Actor, ActorSettings } from "../lib/schema";
import { Button, ErrorBanner, Modal } from "./ui";

export function Composer({ actor, actorSettings, onClose, onSubmit }: { actor: Actor; actorSettings?: ActorSettings; onClose: () => void; onSubmit: (text: string, visibility: string, contentWarning?: string) => Promise<void> }) {
    const [text, setText] = useState("");
    const [visibility, setVisibility] = useState(actorSettings?.default_visibility || "public");
    const [useCW, setUseCW] = useState(false);
    const [cw, setCW] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const submit = async (event: FormEvent) => {
        event.preventDefault();
        if (!text.trim()) return;
        setBusy(true);
        setError("");
        try {
            await onSubmit(text.trim(), visibility, useCW ? cw.trim() : undefined);
            onClose();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "投稿できませんでした");
        } finally {
            setBusy(false);
        }
    };
    return (
        <Modal label="ノートを作成" onClose={onClose}>
            <form className="composer" onSubmit={submit}>
                <header>
                    <div>
                        <p className="eyebrow">@{actor.username}として</p>
                        <h2>新しいノート</h2>
                    </div>
                </header>
                {error && <ErrorBanner message={error} />}
                <textarea aria-label="ノート本文" autoFocus maxLength={3000} onChange={(event) => setText(event.target.value)} placeholder="いまどうしてる？" rows={7} value={text} />
                {useCW && <input aria-label="内容の注記" className="composer__cw" maxLength={500} onChange={(event) => setCW(event.target.value)} placeholder="内容の注記" value={cw} />}
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
                    </div>
                    <Button disabled={busy || !text.trim()} type="submit">
                        <IconSend />
                        投稿する
                    </Button>
                </footer>
            </form>
        </Modal>
    );
}
