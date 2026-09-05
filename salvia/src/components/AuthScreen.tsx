import { type CSSProperties, type FormEvent, useState } from "react";

import { IconArrowRight, IconKey, IconLeaf2, IconShieldCheck } from "@tabler/icons-react";

import { ApiError, api } from "../lib/api";
import { css } from "../lib/css";
import { createPasskey, getPasskey } from "../lib/webauthn";
import { Button, ErrorBanner } from "./ui";

const styles = {
    page: { minHeight: "100dvh", padding: "2.5rem 1.25rem", display: "grid", placeItems: "center", background: "radial-gradient(circle at 50% 0, var(--accent-soft), transparent 38%), var(--page)" },
    wrap: { width: "100%", maxWidth: "28rem" },
    intro: { marginBottom: "1.75rem", display: "flex", flexDirection: "column", alignItems: "center", textAlign: "center" },
    mark: { width: "2.75rem", height: "2.75rem", display: "grid", placeItems: "center", borderRadius: "1rem", color: "var(--accent-ink)", background: "linear-gradient(135deg, #f8d56a, var(--accent))", boxShadow: "0 8px 22px #e9a91d3d" },
    title: { marginTop: "1rem", fontSize: "1.875rem", lineHeight: 1.2, fontWeight: 900, letterSpacing: "-0.025em" },
    tagline: { maxWidth: "20rem", marginTop: "0.5rem", color: "var(--muted)", fontSize: "0.875rem", lineHeight: "1.5rem" },
    card: { width: "100%", padding: "1.5rem", border: "1px solid var(--border)", borderRadius: "1.5rem", background: "var(--panel)", boxShadow: "0 20px 25px -5px #0000001a, 0 8px 10px -6px #0000001a" },
    heading: { marginBottom: "1.5rem", display: "flex", alignItems: "center", gap: "0.75rem" },
    headingMark: { width: "2.5rem", height: "2.5rem", display: "grid", placeItems: "center", borderRadius: "1rem", color: "var(--accent-hover)", background: "var(--accent-soft)" },
    headingTitle: { fontWeight: 900 },
    headingText: { color: "var(--muted)", fontSize: "0.75rem" },
    field: { display: "block", marginBottom: "1rem" },
    fieldLabel: { display: "block", marginBottom: "0.375rem", fontSize: "0.875rem", fontWeight: 700 },
    input: { width: "100%", padding: "0.625rem 1rem", borderWidth: 1, borderStyle: "solid", borderRadius: "1rem", outline: "none", color: "var(--text)", transition: "border-color 150ms, background-color 150ms" },
    hint: { marginTop: "1rem", color: "var(--muted)", textAlign: "center", fontSize: "0.75rem", lineHeight: "1.25rem" },
} satisfies Record<string, CSSProperties>;

const rules = {
    mark: css({ "& svg": { width: "1.5rem", height: "1.5rem" } }),
    headingMark: css({ "& svg": { width: "1.25rem", height: "1.25rem" } }),
    input: css({ borderColor: "var(--border)", background: "var(--panel-muted)", "&:focus": { borderColor: "var(--accent-hover)", background: "var(--panel)" } }),
};

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
        <main style={styles.page}>
            <div style={styles.wrap}>
                <header style={styles.intro}>
                    <span className={rules.mark} style={styles.mark}>
                        <IconLeaf2 />
                    </span>
                    <h1 style={styles.title}>Salvia</h1>
                    <p style={styles.tagline}>Rosmarinusのための、軽やかなソーシャルクライアント</p>
                </header>
                <form onSubmit={submit} style={styles.card}>
                    <div style={styles.heading}>
                        <span className={rules.headingMark} style={styles.headingMark}>
                            <IconShieldCheck />
                        </span>
                        <div>
                            <h2 style={styles.headingTitle}>{mode === "setup" ? "初期セットアップ" : "おかえりなさい"}</h2>
                            <p style={styles.headingText}>{mode === "setup" ? "最初の管理者とパスキーを作成します" : "パスキーで安全にログインします"}</p>
                        </div>
                    </div>
                    {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                    {mode === "setup" && (
                        <>
                            <label style={styles.field}>
                                <span style={styles.fieldLabel}>ユーザー名</span>
                                <input autoComplete="username" autoFocus className={rules.input} maxLength={64} onChange={(event) => setUsername(event.target.value)} placeholder="admin" required style={styles.input} value={username} />
                            </label>
                            <label style={styles.field}>
                                <span style={styles.fieldLabel}>表示名</span>
                                <input autoComplete="name" className={rules.input} maxLength={128} onChange={(event) => setDisplayName(event.target.value)} placeholder="Administrator" style={styles.input} value={displayName} />
                            </label>
                        </>
                    )}
                    <Button disabled={busy || (mode === "setup" && !username.trim())} style={{ width: "100%" }} type="submit">
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
                    <p style={styles.hint}>パスワードは使用しません。端末の画面ロックを使って認証します。</p>
                </form>
            </div>
        </main>
    );
}
