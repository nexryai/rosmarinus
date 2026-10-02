import { type CSSProperties, type FormEvent, useEffect, useState } from "react";

import { IconKey, IconPlus, IconTrash } from "@tabler/icons-react";

import { Button, ErrorBanner } from "@/components/ui";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { api } from "@/lib/api";
import { css } from "@/lib/css";
import type { Passkey } from "@/lib/schema";
import { createPasskey } from "@/lib/webauthn";

const styles = {
    card: {
        padding: "1.25rem",
        border: "1px solid var(--border)",
        borderRadius: "1.5rem",
        background: "var(--panel)",
    },
    heading: {
        display: "flex",
        alignItems: "center",
        gap: "0.625rem",
        marginBottom: "0.5rem",
        fontSize: "1.125rem",
        fontWeight: 900,
    },
    intro: {
        marginBottom: "1.25rem",
        color: "var(--muted)",
        fontSize: "0.875rem",
    },
    list: {
        display: "grid",
        gap: "0.75rem",
        marginBottom: "1.5rem",
    },
    item: {
        padding: "1rem",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
    },
    itemMeta: {
        marginBlock: "0.375rem 0.875rem",
        color: "var(--muted)",
        fontSize: "0.75rem",
        overflowWrap: "anywhere",
    },
    form: {
        display: "flex",
        gap: "0.75rem",
        flexWrap: "wrap",
        alignItems: "end",
    },
    field: {
        display: "grid",
        gap: "0.375rem",
        flex: "1 1 12rem",
        fontSize: "0.875rem",
        fontWeight: 700,
    },
    input: {
        width: "100%",
        minHeight: "2.5rem",
        padding: "0.5rem 0.75rem",
        border: "1px solid var(--border)",
        borderRadius: "0.75rem",
        background: "var(--panel-muted)",
        color: "var(--text)",
    },
    status: {
        marginBlock: "0.75rem",
        color: "var(--muted)",
        fontSize: "0.875rem",
    },
    success: {
        marginBlock: "0.75rem",
        color: "#287a48",
        fontSize: "0.875rem",
    },
    actions: {
        display: "flex",
        gap: "0.5rem",
        flexWrap: "wrap",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    card: css({
        "@media (width >= 40rem)": {
            padding: "1.5rem",
        },
    }),
};

export function SecuritySettings({ csrf }: { csrf: string }) {
    const [passkeys, setPasskeys] = useState<Passkey[]>();
    const [names, setNames] = useState<Record<string, string>>({});
    const [newName, setNewName] = useState("");
    const [busy, setBusy] = useState("");
    const [deleteID, setDeleteID] = useState("");
    const [error, setError] = useState("");
    const [message, setMessage] = useState("");

    useEffect(() => {
        const controller = new AbortController();
        api.passkeys(controller.signal)
            .then((items) => {
                setPasskeys(items);
                setNames(Object.fromEntries(items.map((item) => [item.id, item.name])));
            })
            .catch((reason) => {
                if (!controller.signal.aborted) setError(reason instanceof Error ? reason.message : "パスキーを読み込めませんでした");
            });
        return () => controller.abort();
    }, []);

    const register = async (event: FormEvent) => {
        event.preventDefault();
        if (busy) return;
        setBusy("register");
        setError("");
        setMessage("");
        try {
            const options = await api.passkeyRegistrationStart(csrf, newName.trim());
            const credential = await createPasskey(options.public_key);
            const items = await api.passkeyRegistrationFinish(csrf, options.ceremony_id, credential);
            setPasskeys(items);
            setNames(Object.fromEntries(items.map((item) => [item.id, item.name])));
            setNewName("");
            setMessage("パスキーを登録しました");
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "パスキーを登録できませんでした");
        } finally {
            setBusy("");
        }
    };

    const rename = async (item: Passkey) => {
        const name = names[item.id]?.trim() ?? "";
        if (!name || name === item.name || busy) return;
        setBusy(item.id);
        setError("");
        try {
            await api.renamePasskey(csrf, item.id, name);
            setPasskeys((current) => current?.map((value) => (value.id === item.id ? { ...value, name } : value)));
            setMessage("パスキーの名前を変更しました");
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "名前を変更できませんでした");
        } finally {
            setBusy("");
        }
    };

    const remove = async () => {
        if (!deleteID || busy) return;
        setBusy(deleteID);
        setError("");
        try {
            await api.deletePasskey(csrf, deleteID);
            setPasskeys((current) => current?.filter((item) => item.id !== deleteID));
            setDeleteID("");
            setMessage("パスキーを削除しました");
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "パスキーを削除できませんでした");
        } finally {
            setBusy("");
        }
    };

    return (
        <>
            <section className={rules.card} style={styles.card}>
                <h2 style={styles.heading}>
                    <IconKey size={22} />
                    パスキー
                </h2>
                <p style={styles.intro}>ログインに使うパスキーを複数登録できます。別の端末やセキュリティキーを追加しておくと、ひとつを使えなくなってもログインできます。</p>
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                {message && (
                    <p role="status" style={styles.success}>
                        {message}
                    </p>
                )}
                {!passkeys && !error && (
                    <p role="status" style={styles.status}>
                        パスキーを読み込み中…
                    </p>
                )}
                {passkeys && (
                    <div style={styles.list}>
                        {passkeys.map((item) => (
                            <div key={item.id} style={styles.item}>
                                <form
                                    onSubmit={(event) => {
                                        event.preventDefault();
                                        void rename(item);
                                    }}
                                    style={styles.form}
                                >
                                    <label style={styles.field}>
                                        パスキーの名前
                                        <input aria-label={`${item.name}の名前`} maxLength={64} onChange={(event) => setNames((current) => ({ ...current, [item.id]: event.target.value }))} required style={styles.input} value={names[item.id] ?? item.name} />
                                    </label>
                                    <div style={styles.actions}>
                                        <Button disabled={!!busy || !names[item.id]?.trim() || names[item.id]?.trim() === item.name} type="submit" variant="secondary">
                                            名前を保存
                                        </Button>
                                        <Button aria-label={`${item.name}を削除`} disabled={!!busy || passkeys.length <= 1} onClick={() => setDeleteID(item.id)} type="button" variant="danger">
                                            <IconTrash size={18} />
                                            削除
                                        </Button>
                                    </div>
                                </form>
                                <p style={styles.itemMeta}>
                                    登録: {item.created_at ? new Date(item.created_at).toLocaleString() : "不明"} · ID: …{item.id.slice(-10)}
                                </p>
                            </div>
                        ))}
                        {passkeys.length === 1 && <p style={styles.status}>最後のパスキーは削除できません。</p>}
                    </div>
                )}
                <form onSubmit={register} style={styles.form}>
                    <label style={styles.field}>
                        新しいパスキーの名前
                        <input maxLength={64} onChange={(event) => setNewName(event.target.value)} placeholder="例: スマートフォン" required style={styles.input} value={newName} />
                    </label>
                    <Button disabled={!!busy || !passkeys} type="submit">
                        <IconPlus size={18} />
                        パスキーを追加
                    </Button>
                </form>
            </section>
            {deleteID && (
                <ConfirmDialog busy={!!busy} confirmLabel="削除する" onCancel={() => setDeleteID("")} onConfirm={() => void remove()} title="パスキーを削除しますか？">
                    このパスキーではログインできなくなります。
                </ConfirmDialog>
            )}
        </>
    );
}
