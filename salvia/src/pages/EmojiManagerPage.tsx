import { type CSSProperties, type FormEvent, useCallback, useEffect, useRef, useState } from "react";

import { IconDownload, IconMoodSmile, IconPencil, IconPlus, IconSearch, IconTrash } from "@tabler/icons-react";

import { CustomEmoji } from "../components/EmojiText";
import { ImageFileInput } from "../components/ImageFileInput";
import { Button, Empty, ErrorBanner, Loading, Modal, PageHeader } from "../components/ui";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { api } from "../lib/api";
import { css } from "../lib/css";
import { createCanvasThumbnail, revokeCanvasThumbnail } from "../lib/image";
import type { ManagedEmoji } from "../lib/schema";
import { uploadImage } from "../lib/uploader";

type Scope = "local" | "remote";

const styles = {
    headerAction: {
        minHeight: "2.25rem",
        marginLeft: "auto",
    },
    headerIcon: {
        width: "1.5rem",
        height: "1.5rem",
        marginLeft: "auto",
    },
    content: {
        padding: "1.25rem",
        display: "grid",
        gap: "1rem",
    },
    mobileToolbar: {
        alignItems: "center",
        justifyContent: "space-between",
        gap: "1rem",
    },
    mobileTitle: {
        margin: 0,
        fontSize: "1.25rem",
    },
    tabs: {
        display: "flex",
        gap: "0.5rem",
        borderBottom: "1px solid var(--border)",
    },
    tab: {
        padding: "0.75rem 1rem",
        borderBottom: "2px solid transparent",
        color: "var(--muted)",
        fontWeight: 800,
    },
    tabActive: {
        borderBottomColor: "var(--accent-hover)",
        color: "var(--text)",
    },
    filters: {
        display: "grid",
        gap: "0.625rem",
    },
    input: {
        width: "100%",
        minHeight: "2.75rem",
        padding: "0.625rem 0.875rem",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        outline: "none",
        color: "var(--text)",
        background: "var(--panel-muted)",
    },
    grid: {
        display: "grid",
        gridTemplateColumns: "repeat(auto-fill, minmax(13rem, 1fr))",
        gap: "0.75rem",
    },
    card: {
        minWidth: 0,
        padding: "0.875rem",
        display: "grid",
        gridTemplateColumns: "3.25rem minmax(0, 1fr)",
        alignItems: "center",
        gap: "0.75rem",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        background: "var(--panel-muted)",
    },
    emoji: {
        width: "3.25rem",
        height: "3.25rem",
        margin: 0,
    },
    metadata: {
        minWidth: 0,
    },
    emojiName: {
        display: "block",
        overflow: "hidden",
        fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
        fontSize: "0.875rem",
        fontWeight: 800,
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    host: {
        display: "block",
        marginTop: "0.125rem",
        overflow: "hidden",
        color: "var(--muted)",
        fontSize: "0.75rem",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    cardActions: {
        gridColumn: "1 / -1",
        display: "flex",
        justifyContent: "flex-end",
        gap: "0.5rem",
    },
    editor: {
        display: "grid",
        gap: "1rem",
    },
    editorTitle: {
        margin: "0 2.5rem 0 0",
        fontSize: "1.25rem",
    },
    label: {
        display: "grid",
        gap: "0.375rem",
        fontSize: "0.875rem",
        fontWeight: 800,
    },
    editorActions: {
        display: "flex",
        justifyContent: "flex-end",
        gap: "0.625rem",
    },
    loadMore: {
        justifySelf: "center",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    input: css({
        "&:focus": {
            borderColor: "var(--accent-hover)",
            boxShadow: "0 0 0 3px var(--accent-soft)",
        },
    }),
    localFilters: css({
        gridTemplateColumns: "minmax(0, 1fr) auto",
        "@media (width < 40rem)": {
            gridTemplateColumns: "1fr",
        },
    }),
    remoteFilters: css({
        gridTemplateColumns: "minmax(0, 2fr) minmax(0, 1fr) auto",
        "@media (width < 40rem)": {
            gridTemplateColumns: "1fr",
        },
    }),
    mobileToolbar: css({
        display: "flex",
        "@media (width >= 64rem)": {
            display: "none",
        },
    }),
};

function EmojiEditor({ actorID, csrf, emoji, onClose, onSaved }: { actorID: string; csrf: string; emoji?: ManagedEmoji; onClose: () => void; onSaved: (emoji: ManagedEmoji) => void }) {
    const [name, setName] = useState(emoji?.name || "");
    const [file, setFile] = useState<File>();
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const submit = async (event: FormEvent) => {
        event.preventDefault();
        if (!emoji && !file) return;
        setBusy(true);
        setError("");
        let thumbnail: Awaited<ReturnType<typeof createCanvasThumbnail>> | null = null;
        let mediaID = "";
        let committed = false;
        try {
            if (file) {
                thumbnail = await createCanvasThumbnail(file);
                mediaID = (await uploadImage(csrf, actorID, file, { width: thumbnail.originalWidth, height: thumbnail.originalHeight })).id;
            }
            const saved = emoji ? await api.updateEmoji(csrf, actorID, emoji.id, name.trim(), mediaID) : await api.createEmoji(csrf, actorID, name.trim(), mediaID);
            committed = true;
            onSaved(saved);
        } catch (reason) {
            if (mediaID && !committed) await api.deleteMedia(csrf, actorID, mediaID).catch(() => undefined);
            setError(reason instanceof Error ? reason.message : "絵文字を保存できませんでした");
        } finally {
            revokeCanvasThumbnail(thumbnail);
            setBusy(false);
        }
    };
    return (
        <Modal label={emoji ? "カスタム絵文字を編集" : "カスタム絵文字を追加"} onClose={() => !busy && onClose()}>
            <form onSubmit={(event) => void submit(event)} style={styles.editor}>
                <h2 style={styles.editorTitle}>{emoji ? "カスタム絵文字を編集" : "カスタム絵文字を追加"}</h2>
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                <label style={styles.label}>
                    <span>名前</span>
                    <input autoCapitalize="none" autoComplete="off" className={rules.input} maxLength={100} onChange={(event) => setName(event.target.value)} pattern="[A-Za-z0-9_]+" required spellCheck={false} style={styles.input} value={name} />
                </label>
                <label htmlFor="emoji-image" style={styles.label}>
                    <span>{emoji ? "画像（変更する場合のみ）" : "画像"}</span>
                    <ImageFileInput disabled={busy} id="emoji-image" onSelect={(files) => setFile(files[0])} required={!emoji} resetAfterSelect={false} />
                </label>
                <footer style={styles.editorActions}>
                    <Button disabled={busy} onClick={onClose} type="button" variant="ghost">
                        キャンセル
                    </Button>
                    <Button disabled={busy || !name.trim() || (!emoji && !file)} type="submit">
                        {busy ? "保存中" : "保存"}
                    </Button>
                </footer>
            </form>
        </Modal>
    );
}

function ImportEditor({ actorID, csrf, emoji, onClose, onImported }: { actorID: string; csrf: string; emoji: ManagedEmoji; onClose: () => void; onImported: (emoji: ManagedEmoji) => void }) {
    const [name, setName] = useState(emoji.name);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const submit = async (event: FormEvent) => {
        event.preventDefault();
        setBusy(true);
        setError("");
        try {
            onImported(await api.importEmoji(csrf, actorID, emoji.id, name.trim()));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "絵文字をインポートできませんでした");
        } finally {
            setBusy(false);
        }
    };
    return (
        <Modal label="リモート絵文字をインポート" onClose={() => !busy && onClose()}>
            <form onSubmit={(event) => void submit(event)} style={styles.editor}>
                <h2 style={styles.editorTitle}>リモート絵文字をインポート</h2>
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                <CustomEmoji emoji={emoji} style={styles.emoji} />
                <span style={styles.host}>{emoji.host}</span>
                <label style={styles.label}>
                    <span>ローカルでの名前</span>
                    <input autoCapitalize="none" className={rules.input} maxLength={100} onChange={(event) => setName(event.target.value)} pattern="[A-Za-z0-9_]+" required spellCheck={false} style={styles.input} value={name} />
                </label>
                <footer style={styles.editorActions}>
                    <Button disabled={busy} onClick={onClose} type="button" variant="ghost">
                        キャンセル
                    </Button>
                    <Button disabled={busy || !name.trim()} type="submit">
                        <IconDownload />
                        {busy ? "インポート中" : "インポート"}
                    </Button>
                </footer>
            </form>
        </Modal>
    );
}

export function EmojiManagerPage({ actorID, csrf, onCatalogChanged }: { actorID: string; csrf: string; onCatalogChanged: () => void }) {
    const [scope, setScope] = useState<Scope>("local");
    const [items, setItems] = useState<ManagedEmoji[]>([]);
    const [next, setNext] = useState("");
    const [queryInput, setQueryInput] = useState("");
    const [hostInput, setHostInput] = useState("");
    const [filters, setFilters] = useState({ query: "", host: "" });
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState("");
    const [editorOpen, setEditorOpen] = useState(false);
    const [editing, setEditing] = useState<ManagedEmoji>();
    const [importing, setImporting] = useState<ManagedEmoji>();
    const [deleting, setDeleting] = useState<ManagedEmoji>();
    const [busy, setBusy] = useState(false);
    const requestVersion = useRef(0);
    const load = useCallback(
        async (after = "") => {
            const version = ++requestVersion.current;
            setLoading(true);
            setError("");
            try {
                const page = await api.emojiCatalog(scope, { after, query: filters.query, host: scope === "remote" ? filters.host : "" });
                if (version !== requestVersion.current) return;
                setItems((current) => (after ? [...current, ...page.data] : page.data));
                setNext(page.next);
            } catch (reason) {
                if (version !== requestVersion.current) return;
                setError(reason instanceof Error ? reason.message : "絵文字を読み込めませんでした");
            } finally {
                if (version === requestVersion.current) setLoading(false);
            }
        },
        [filters, scope],
    );
    useEffect(() => {
        void load();
    }, [load]);
    const switchScope = (value: Scope) => {
        setScope(value);
        setItems([]);
        setNext("");
        setFilters({ query: "", host: "" });
        setQueryInput("");
        setHostInput("");
    };
    const saved = (emoji: ManagedEmoji) => {
        setItems((current) => {
            const exists = current.some((item) => item.id === emoji.id);
            return exists ? current.map((item) => (item.id === emoji.id ? emoji : item)) : [emoji, ...current];
        });
        setEditorOpen(false);
        setEditing(undefined);
        onCatalogChanged();
    };
    return (
        <>
            <PageHeader
                title="カスタム絵文字"
                trailing={
                    scope === "local" ? (
                        <Button
                            onClick={() => {
                                setEditing(undefined);
                                setEditorOpen(true);
                            }}
                            style={styles.headerAction}
                        >
                            <IconPlus />
                            追加
                        </Button>
                    ) : (
                        <IconMoodSmile style={styles.headerIcon} />
                    )
                }
            />
            <div style={styles.content}>
                {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
                <div className={rules.mobileToolbar} style={styles.mobileToolbar}>
                    <h1 style={styles.mobileTitle}>カスタム絵文字</h1>
                    {scope === "local" && (
                        <Button
                            onClick={() => {
                                setEditing(undefined);
                                setEditorOpen(true);
                            }}
                        >
                            <IconPlus />
                            追加
                        </Button>
                    )}
                </div>
                <div aria-label="絵文字の種類" role="tablist" style={styles.tabs}>
                    {(["local", "remote"] as const).map((value) => (
                        <button aria-selected={scope === value} key={value} onClick={() => switchScope(value)} role="tab" style={{ ...styles.tab, ...(scope === value ? styles.tabActive : {}) }} type="button">
                            {value === "local" ? "ローカル" : "リモート"}
                        </button>
                    ))}
                </div>
                <form
                    className={scope === "remote" ? rules.remoteFilters : rules.localFilters}
                    onSubmit={(event) => {
                        event.preventDefault();
                        setFilters({ query: queryInput.trim(), host: hostInput.trim() });
                    }}
                    style={styles.filters}
                >
                    <input aria-label="絵文字名で検索" className={rules.input} onChange={(event) => setQueryInput(event.target.value)} placeholder="絵文字名" style={styles.input} type="search" value={queryInput} />
                    {scope === "remote" && <input aria-label="ホストで絞り込み" className={rules.input} onChange={(event) => setHostInput(event.target.value)} placeholder="example.com" style={styles.input} value={hostInput} />}
                    <Button type="submit" variant="ghost">
                        <IconSearch />
                        検索
                    </Button>
                </form>
                {loading && items.length === 0 ? (
                    <Loading />
                ) : items.length === 0 ? (
                    <Empty>{scope === "local" ? "ローカル絵文字はまだありません。" : "観測済みのリモート絵文字はありません。"}</Empty>
                ) : (
                    <div style={styles.grid}>
                        {items.map((emoji) => (
                            <article key={emoji.id} style={styles.card}>
                                <CustomEmoji emoji={emoji} style={styles.emoji} />
                                <div style={styles.metadata}>
                                    <span style={styles.emojiName}>:{emoji.name}:</span>
                                    <span style={styles.host}>{emoji.host || "このサーバー"}</span>
                                </div>
                                <div style={styles.cardActions}>
                                    {scope === "local" ? (
                                        <>
                                            <Button
                                                onClick={() => {
                                                    setEditing(emoji);
                                                    setEditorOpen(true);
                                                }}
                                                variant="ghost"
                                            >
                                                <IconPencil />
                                                編集
                                            </Button>
                                            <Button onClick={() => setDeleting(emoji)} variant="danger">
                                                <IconTrash />
                                                削除
                                            </Button>
                                        </>
                                    ) : (
                                        <Button onClick={() => setImporting(emoji)}>
                                            <IconDownload />
                                            インポート
                                        </Button>
                                    )}
                                </div>
                            </article>
                        ))}
                    </div>
                )}
                {next && (
                    <Button disabled={loading} onClick={() => void load(next)} style={styles.loadMore} variant="ghost">
                        {loading ? "読み込み中" : "さらに表示"}
                    </Button>
                )}
            </div>
            {editorOpen && <EmojiEditor actorID={actorID} csrf={csrf} emoji={editing} onClose={() => setEditorOpen(false)} onSaved={saved} />}
            {importing && (
                <ImportEditor
                    actorID={actorID}
                    csrf={csrf}
                    emoji={importing}
                    onClose={() => setImporting(undefined)}
                    onImported={() => {
                        setImporting(undefined);
                        onCatalogChanged();
                        setItems([]);
                        setScope("local");
                        setFilters({ query: "", host: "" });
                    }}
                />
            )}
            {deleting && (
                <ConfirmDialog
                    busy={busy}
                    confirmLabel="削除"
                    onCancel={() => setDeleting(undefined)}
                    onConfirm={() => {
                        const target = deleting;
                        setBusy(true);
                        void api
                            .deleteEmoji(csrf, target.id)
                            .then(() => {
                                setItems((current) => current.filter((item) => item.id !== target.id));
                                onCatalogChanged();
                            })
                            .catch((reason) => setError(reason instanceof Error ? reason.message : "絵文字を削除できませんでした"))
                            .finally(() => {
                                setBusy(false);
                                setDeleting(undefined);
                            });
                    }}
                    title="カスタム絵文字を削除しますか？"
                >
                    :{deleting.name}: を削除します。この操作後、過去のノートでは絵文字が表示されなくなる場合があります。
                </ConfirmDialog>
            )}
        </>
    );
}
