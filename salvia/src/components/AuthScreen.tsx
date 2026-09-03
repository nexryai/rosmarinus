import { type FormEvent, useState } from "react";

import { IconArrowRight, IconKey, IconLeaf2, IconShieldCheck } from "@tabler/icons-react";

import { ApiError, api } from "../lib/api";
import { createPasskey, getPasskey } from "../lib/webauthn";
import { Button, ErrorBanner } from "./ui";

export function AuthScreen({ mode, onAuthenticated }: { mode: "login" | "setup"; onAuthenticated: () => Promise<void> }) {
    const [username, setUsername] = useState("");
    const [displayName, setDisplayName] = useState("");
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");

    const submit = async (event: FormEvent) => {
        event.preventDefault();
        setBusy(true);
        setError("");
        try {
            if (mode === "setup") {
                const start = await api.setupStart(username.trim(), displayName.trim());
                const credential = await createPasskey(start.data.public_key);
                await api.setupFinish(start.data.ceremony_id, credential);
            } else {
                const start = await api.loginStart();
                const credential = await getPasskey(start.data.public_key);
                await api.loginFinish(start.data.ceremony_id, credential);
            }
            await onAuthenticated();
        } catch (reason) {
            setError(reason instanceof ApiError ? reason.message : reason instanceof Error ? reason.message : "パスキー操作に失敗しました");
        } finally {
            setBusy(false);
        }
    };

    return (
        <main className="auth-page">
            <div className="auth-card-wrap">
                <header className="brand-intro">
                    <span className="brand-mark">
                        <IconLeaf2 />
                    </span>
                    <h1>Salvia</h1>
                    <p>Rosmarinusのための、軽やかなソーシャルクライアント</p>
                </header>
                <form className="auth-card" onSubmit={submit}>
                    <div className="section-heading">
                        <span>
                            <IconShieldCheck />
                        </span>
                        <div>
                            <h2>{mode === "setup" ? "初期セットアップ" : "おかえりなさい"}</h2>
                            <p>{mode === "setup" ? "最初の管理者とパスキーを作成します" : "パスキーで安全にログインします"}</p>
                        </div>
                    </div>
                    {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                    {mode === "setup" && (
                        <>
                            <label className="field">
                                <span>ユーザー名</span>
                                <input autoComplete="username" autoFocus maxLength={64} onChange={(event) => setUsername(event.target.value)} placeholder="admin" required value={username} />
                            </label>
                            <label className="field">
                                <span>表示名</span>
                                <input autoComplete="name" maxLength={128} onChange={(event) => setDisplayName(event.target.value)} placeholder="Administrator" value={displayName} />
                            </label>
                        </>
                    )}
                    <Button className="button--wide" disabled={busy || (mode === "setup" && !username.trim())} type="submit">
                        {mode === "setup" ? (
                            <>
                                <IconKey />
                                パスキーを登録
                            </>
                        ) : (
                            <>
                                <IconKey />
                                パスキーでログイン
                            </>
                        )}
                        <IconArrowRight />
                    </Button>
                    <p className="auth-card__hint">パスワードは使用しません。端末の画面ロックを使って認証します。</p>
                </form>
            </div>
        </main>
    );
}
