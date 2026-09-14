import { type CSSProperties, useCallback, useEffect, useState } from "react";

import { IconAntenna, IconEdit, IconPlus, IconTrash } from "@tabler/icons-react";

import { NoteCard } from "../components/NoteCard";
import { NoteList, NoteListItem } from "../components/NoteList";
import { Button, Empty, ErrorBanner, Loading, Modal, PageHeader } from "../components/ui";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { Dropdown } from "../components/ui/Dropdown";
import { Switch } from "../components/ui/Switch";
import { type AntennaInput, api } from "../lib/api";
import { css } from "../lib/css";
import type { Antenna, Emoji, Note } from "../lib/schema";

const emptyInput: AntennaInput = { name: "", source: "all", users: [], keywords: [], exclude_keywords: [], case_sensitive: false, local_only: false, exclude_bots: false, with_replies: true, with_file: false };

const styles = {
    headerIcon: { width: "1.5rem", height: "1.5rem", color: "var(--accent-hover)" },
    toolbar: { padding: "1rem 1.25rem", display: "flex", alignItems: "center", justifyContent: "space-between", gap: "1rem", borderBottom: "1px solid var(--border)" },
    selector: { minWidth: "12rem", maxWidth: "24rem", flex: 1 },
    editor: { padding: "1.5rem", display: "grid", gap: "1rem" },
    title: { paddingRight: "2.5rem", fontSize: "1.25rem" },
    label: { display: "grid", gap: "0.5rem", fontSize: "0.875rem", fontWeight: 700 },
    input: { width: "100%", padding: "0.75rem 1rem", border: "1px solid var(--border)", borderRadius: "0.875rem", color: "var(--text)", background: "var(--panel-muted)", outline: "none", resize: "vertical" },
    hint: { color: "var(--muted)", fontSize: "0.75rem", fontWeight: 400, lineHeight: 1.5 },
    toggles: { display: "grid", gap: "0.875rem" },
    editorActions: { display: "flex", justifyContent: "flex-end", gap: "0.75rem" },
    antennaActions: { display: "flex", gap: "0.5rem" },
    loadMore: { padding: "1.5rem", display: "flex", justifyContent: "center" },
} satisfies Record<string, CSSProperties>;

const rules = { input: css({ "&:focus": { borderColor: "var(--accent-hover)", boxShadow: "0 0 0 3px var(--accent-soft)" } }) };

const groupsToText = (groups: string[][]) => groups.map((group) => group.join(" ")).join("\n");
const textToGroups = (value: string) =>
    value
        .split("\n")
        .map((line) => line.trim().split(/\s+/).filter(Boolean))
        .filter((group) => group.length > 0);
const antennaInput = (antenna: Antenna): AntennaInput => ({
    name: antenna.name,
    source: antenna.source,
    users: antenna.users,
    keywords: antenna.keywords,
    exclude_keywords: antenna.exclude_keywords,
    case_sensitive: antenna.case_sensitive,
    local_only: antenna.local_only,
    exclude_bots: antenna.exclude_bots,
    with_replies: antenna.with_replies,
    with_file: antenna.with_file,
});

function AntennaEditor({ antenna, busy, onClose, onSave }: { antenna?: Antenna; busy: boolean; onClose: () => void; onSave: (input: AntennaInput) => void }) {
    const [input, setInput] = useState<AntennaInput>(() => (antenna ? antennaInput(antenna) : emptyInput));
    const [usersText, setUsersText] = useState(() => antenna?.users.join("\n") || "");
    const [keywordsText, setKeywordsText] = useState(() => groupsToText(antenna?.keywords || []));
    const [excludeKeywordsText, setExcludeKeywordsText] = useState(() => groupsToText(antenna?.exclude_keywords || []));
    const patch = <K extends keyof AntennaInput>(key: K, value: AntennaInput[K]) => setInput((current) => ({ ...current, [key]: value }));
    const valid = input.name.trim() && (input.keywords.length > 0 || input.exclude_keywords.length > 0) && (input.source !== "users" || input.users.length > 0);
    return (
        <Modal label={antenna ? "アンテナを編集" : "アンテナを作成"} onClose={onClose}>
            <form
                onSubmit={(event) => {
                    event.preventDefault();
                    if (valid) onSave(input);
                }}
                style={styles.editor}
            >
                <h2 style={styles.title}>{antenna ? "アンテナを編集" : "新しいアンテナ"}</h2>
                <label style={styles.label}>
                    名前
                    <input className={rules.input} maxLength={100} onChange={(event) => patch("name", event.target.value)} required style={styles.input} value={input.name} />
                </label>
                <div style={styles.label}>
                    対象
                    <Dropdown
                        label="対象ユーザー"
                        onChange={(source) => patch("source", source)}
                        options={[
                            { value: "all", label: "すべてのユーザー" },
                            { value: "users", label: "指定したユーザーのみ" },
                            { value: "users_blacklist", label: "指定したユーザーを除外" },
                        ]}
                        value={input.source}
                    />
                </div>
                {input.source !== "all" && (
                    <label style={styles.label}>
                        ユーザー（1行に1ハンドルまたはURL）
                        <textarea
                            className={rules.input}
                            onChange={(event) => {
                                setUsersText(event.target.value);
                                patch(
                                    "users",
                                    event.target.value
                                        .split("\n")
                                        .map((value) => value.trim())
                                        .filter(Boolean),
                                );
                            }}
                            placeholder="@alice@example.com"
                            rows={3}
                            style={styles.input}
                            value={usersText}
                        />
                    </label>
                )}
                <label style={styles.label}>
                    含めるキーワード
                    <textarea
                        className={rules.input}
                        onChange={(event) => {
                            setKeywordsText(event.target.value);
                            patch("keywords", textToGroups(event.target.value));
                        }}
                        rows={3}
                        style={styles.input}
                        value={keywordsText}
                    />
                    <span style={styles.hint}>同じ行の語はすべて含む場合に一致し、行同士はいずれかに一致すると対象になります。</span>
                </label>
                <label style={styles.label}>
                    除外キーワード
                    <textarea
                        className={rules.input}
                        onChange={(event) => {
                            setExcludeKeywordsText(event.target.value);
                            patch("exclude_keywords", textToGroups(event.target.value));
                        }}
                        rows={2}
                        style={styles.input}
                        value={excludeKeywordsText}
                    />
                </label>
                <div style={styles.toggles}>
                    <Switch checked={input.case_sensitive} label="大文字と小文字を区別" onChange={(value) => patch("case_sensitive", value)} />
                    <Switch checked={input.local_only} label="ローカルのノートのみ" onChange={(value) => patch("local_only", value)} />
                    <Switch checked={input.exclude_bots} label="Botを除外" onChange={(value) => patch("exclude_bots", value)} />
                    <Switch checked={input.with_replies} label="返信を含める" onChange={(value) => patch("with_replies", value)} />
                    <Switch checked={input.with_file} label="添付ファイルがあるノートのみ" onChange={(value) => patch("with_file", value)} />
                </div>
                <div style={styles.editorActions}>
                    <Button onClick={onClose} type="button" variant="ghost">
                        キャンセル
                    </Button>
                    <Button disabled={busy || !valid} type="submit">
                        保存
                    </Button>
                </div>
            </form>
        </Modal>
    );
}

export function AntennaPage({ actorID, csrf, emojis, onCompose, onOpenNote, onOpenProfile, refreshKey = 0 }: { actorID: string; csrf: string; emojis: Emoji[]; onCompose: (kind: "reply" | "quote", note: Note) => void; onOpenNote: (id: string) => void; onOpenProfile: (id: string) => void; refreshKey?: number }) {
    const [antennas, setAntennas] = useState<Antenna[]>([]);
    const [selectedID, setSelectedID] = useState("");
    const [notes, setNotes] = useState<Note[]>([]);
    const [next, setNext] = useState("");
    const [loading, setLoading] = useState(true);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [editor, setEditor] = useState<Antenna | null | undefined>();
    const [deleting, setDeleting] = useState<Antenna>();
    const selected = antennas.find((antenna) => antenna.id === selectedID);
    const loadAntennas = useCallback(async () => {
        setLoading(true);
        try {
            const items = await api.antennas(actorID);
            setAntennas(items);
            setSelectedID((current) => (items.some((item) => item.id === current) ? current : items[0]?.id || ""));
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "アンテナを読み込めませんでした");
        } finally {
            setLoading(false);
        }
    }, [actorID]);
    useEffect(() => {
        void loadAntennas();
    }, [loadAntennas]);
    const loadNotes = useCallback(
        async (after = "") => {
            if (!selectedID) {
                setNotes([]);
                setNext("");
                return;
            }
            setLoading(true);
            try {
                const page = await api.antennaNotes(actorID, selectedID, after);
                setNotes((current) => (after ? [...current, ...page.data.filter((note) => !current.some((item) => item.id === note.id))] : page.data));
                setNext(page.next);
            } catch (reason) {
                setError(reason instanceof Error ? reason.message : "アンテナのノートを読み込めませんでした");
            } finally {
                setLoading(false);
            }
        },
        [actorID, selectedID],
    );
    useEffect(() => {
        void refreshKey;
        void loadNotes();
    }, [loadNotes, refreshKey]);
    const mutate = async (operation: () => Promise<void>) => {
        try {
            await operation();
            await loadNotes();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "操作に失敗しました");
        }
    };
    const save = async (input: AntennaInput) => {
        setBusy(true);
        try {
            if (editor) await api.updateAntenna(csrf, actorID, editor.id, input);
            else await api.createAntenna(csrf, actorID, input);
            setEditor(undefined);
            await loadAntennas();
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "アンテナを保存できませんでした");
        } finally {
            setBusy(false);
        }
    };
    return (
        <>
            <PageHeader eyebrow="キーワードで見つける" title="アンテナ" trailing={<IconAntenna style={styles.headerIcon} />} />
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            <div style={styles.toolbar}>
                {antennas.length > 0 ? <Dropdown label="表示するアンテナ" onChange={setSelectedID} options={antennas.map((antenna) => ({ value: antenna.id, label: antenna.name }))} style={styles.selector} value={selectedID} /> : <span>アンテナはまだありません</span>}
                <div style={styles.antennaActions}>
                    {selected && (
                        <Button aria-label="アンテナを編集" onClick={() => setEditor(selected)} variant="ghost">
                            <IconEdit />
                        </Button>
                    )}
                    {selected && (
                        <Button aria-label="アンテナを削除" onClick={() => setDeleting(selected)} variant="danger">
                            <IconTrash />
                        </Button>
                    )}
                    <Button disabled={antennas.length >= 5} onClick={() => setEditor(null)}>
                        <IconPlus />
                        作成
                    </Button>
                </div>
            </div>
            {loading && notes.length === 0 ? (
                <Loading label="アンテナを読み込み中" />
            ) : !selected ? (
                <Empty>アンテナを作成すると、条件に合うノートをまとめて閲覧できます。</Empty>
            ) : notes.length === 0 ? (
                <Empty>条件に合うノートはまだありません。</Empty>
            ) : (
                <NoteList aria-label={`${selected.name}のノート`}>
                    {notes.map((note) => (
                        <NoteListItem key={note.id}>
                            <NoteCard
                                emojis={emojis}
                                note={note}
                                onDelete={(id) => mutate(() => api.deletePost(csrf, actorID, id))}
                                onOpenNote={onOpenNote}
                                onOpenProfile={onOpenProfile}
                                onQuote={(target) => onCompose("quote", target)}
                                onReact={(id, reaction, reacted) => mutate(() => (reacted ? api.unreact(csrf, actorID, id) : api.react(csrf, actorID, id, reaction)))}
                                onRenote={(target) => mutate(() => api.createPost(csrf, actorID, { renote_id: target.id, visibility: target.visibility }))}
                                onReply={(target) => onCompose("reply", target)}
                                onVote={(id, choice) => mutate(() => api.vote(csrf, actorID, id, choice))}
                                ownActorID={actorID}
                            />
                        </NoteListItem>
                    ))}
                    {next && (
                        <div style={styles.loadMore}>
                            <Button disabled={loading} onClick={() => void loadNotes(next)} variant="secondary">
                                もっと見る
                            </Button>
                        </div>
                    )}
                </NoteList>
            )}
            {editor !== undefined && <AntennaEditor antenna={editor || undefined} busy={busy} onClose={() => setEditor(undefined)} onSave={(input) => void save(input)} />}
            {deleting && (
                <ConfirmDialog
                    busy={busy}
                    confirmLabel="削除"
                    onCancel={() => setDeleting(undefined)}
                    onConfirm={() => {
                        setBusy(true);
                        void api
                            .deleteAntenna(csrf, actorID, deleting.id)
                            .then(() => {
                                setDeleting(undefined);
                                return loadAntennas();
                            })
                            .catch((reason) => setError(reason instanceof Error ? reason.message : "アンテナを削除できませんでした"))
                            .finally(() => setBusy(false));
                    }}
                    title="アンテナを削除しますか？"
                >
                    「{deleting.name}」を削除します。この操作は取り消せません。
                </ConfirmDialog>
            )}
        </>
    );
}
