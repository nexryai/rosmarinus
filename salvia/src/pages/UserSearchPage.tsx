import { type CSSProperties, useState } from "react";

import { IconSearch, IconUserSearch } from "@tabler/icons-react";

import { Button, ErrorBanner, PageHeader } from "../components/ui";
import { api } from "../lib/api";
import { css } from "../lib/css";

const styles = {
    form: { padding: "1.5rem", display: "grid", gap: "1rem" },
    intro: { color: "var(--muted)", lineHeight: 1.7 },
    label: { display: "grid", gap: "0.5rem", fontSize: "0.875rem", fontWeight: 700 },
    input: { width: "100%", padding: "0.75rem 1rem", borderWidth: 1, borderStyle: "solid", borderRadius: "1rem", outline: "none", color: "var(--text)", transition: "border-color 150ms, background-color 150ms" },
    hint: { color: "var(--muted)", fontSize: "0.75rem", fontWeight: 400 },
    submit: { justifySelf: "start" },
    headerIcon: { width: "1.5rem", height: "1.5rem" },
} satisfies Record<string, CSSProperties>;

const rules = {
    input: css({ borderColor: "var(--border)", background: "var(--panel-muted)", "&:focus": { borderColor: "var(--accent-hover)", background: "var(--panel)" } }),
};

export function UserSearchPage({ actorID, csrf, onOpenProfile }: { actorID: string; csrf: string; onOpenProfile: (actorID: string) => void }) {
    const [target, setTarget] = useState("");
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState("");
    const submit = async () => {
        const query = target.trim();
        if (!query) return;
        setLoading(true);
        setError("");
        try {
            const profile = await api.resolveProfile(csrf, actorID, query);
            onOpenProfile(profile.actor.id);
        } catch (reason) {
            setError(reason instanceof Error ? reason.message : "リモートユーザーを見つけられませんでした");
        } finally {
            setLoading(false);
        }
    };
    return (
        <>
            <PageHeader eyebrow="連合ネットワーク" title="ユーザー検索" trailing={<IconUserSearch style={styles.headerIcon} />} />
            {error && <ErrorBanner message={error} onDismiss={() => setError("")} />}
            <form
                onSubmit={(event) => {
                    event.preventDefault();
                    void submit();
                }}
                style={styles.form}
            >
                <p style={styles.intro}>別のサーバーにいるユーザーを検索し、プロフィールからフォローできます。</p>
                <label htmlFor="remote-profile-target" style={styles.label}>
                    <span>ハンドルまたはプロフィールURL</span>
                    <input
                        aria-describedby="remote-profile-target-hint"
                        autoCapitalize="none"
                        autoComplete="off"
                        className={rules.input}
                        disabled={loading}
                        id="remote-profile-target"
                        onChange={(event) => setTarget(event.target.value)}
                        placeholder="@alice@example.com"
                        spellCheck={false}
                        style={styles.input}
                        value={target}
                    />
                </label>
                <span id="remote-profile-target-hint" style={styles.hint}>
                    例: @alice@example.com または https://example.com/users/alice
                </span>
                <Button disabled={loading || !target.trim()} style={styles.submit} type="submit">
                    <IconSearch />
                    {loading ? "検索中" : "プロフィールを表示"}
                </Button>
            </form>
        </>
    );
}
