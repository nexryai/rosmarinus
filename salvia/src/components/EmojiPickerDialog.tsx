import { type CSSProperties, useMemo, useState } from "react";

import { IconSearch } from "@tabler/icons-react";

import { css } from "../lib/css";
import { defaultUnicodeEmojis, searchUnicodeEmojis, type UnicodeEmoji } from "../lib/emojiData";
import type { Emoji } from "../lib/schema";
import { CustomEmoji } from "./EmojiText";
import { Modal } from "./ui";

const maxResults = 100;

const styles = {
    content: {
        paddingRight: "2.75rem",
    },
    title: {
        fontSize: "1.25rem",
        lineHeight: 1.4,
        fontWeight: 900,
    },
    search: {
        position: "relative",
        marginTop: "1rem",
    },
    searchIcon: {
        position: "absolute",
        top: "50%",
        left: "0.875rem",
        width: "1rem",
        height: "1rem",
        color: "var(--muted)",
        transform: "translateY(-50%)",
        pointerEvents: "none",
    },
    input: {
        width: "100%",
        padding: "0.625rem 1rem 0.625rem 2.5rem",
        borderWidth: 1,
        borderStyle: "solid",
        borderRadius: "1rem",
        outline: "none",
        color: "var(--text)",
        transition: "border-color 150ms, background-color 150ms",
    },
    body: {
        maxHeight: "min(24rem, 55dvh)",
        marginTop: "0.75rem",
        overflowY: "auto",
    },
    section: {
        marginTop: "0.5rem",
    },
    sectionTitle: {
        marginBottom: "0.25rem",
        color: "var(--muted)",
        fontSize: "10px",
        fontWeight: 700,
        letterSpacing: "0.1em",
        textTransform: "uppercase",
    },
    grid: {
        display: "grid",
        gridTemplateColumns: "repeat(auto-fill, minmax(2.5rem, 1fr))",
        gap: "0.25rem",
    },
    cell: {
        aspectRatio: "1",
        display: "grid",
        placeItems: "center",
        borderRadius: "0.75rem",
        fontSize: "1.35rem",
        lineHeight: 1,
    },
    customImage: {
        width: "1.75rem",
        height: "1.75rem",
    },
    empty: {
        padding: "1.5rem 0",
        color: "var(--muted)",
        textAlign: "center",
        fontSize: "0.875rem",
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
    cell: css({
        "&:hover": {
            background: "var(--accent-soft)",
        },
    }),
};

const matchesTerms = (name: string, terms: string[]) => {
    const haystack = name.toLowerCase();
    return terms.every((term) => haystack.includes(term));
};

export function EmojiPickerDialog({ emojis, label = "絵文字を選択", onClose, onSelect, title = "絵文字" }: { emojis: Emoji[]; label?: string; onClose: () => void; onSelect: (value: string) => void; title?: string }) {
    const [query, setQuery] = useState("");
    const terms = useMemo(() => query.trim().toLowerCase().split(/\s+/).filter(Boolean), [query]);
    const customMatches = useMemo(() => (terms.length === 0 ? emojis : emojis.filter((emoji) => matchesTerms(emoji.name, terms))).slice(0, maxResults), [emojis, terms]);
    const unicodeMatches = useMemo<UnicodeEmoji[]>(() => (terms.length === 0 ? defaultUnicodeEmojis : searchUnicodeEmojis(terms.join(" "), maxResults)), [terms]);

    const pick = (value: string) => {
        onSelect(value);
        onClose();
    };

    return (
        <Modal label={label} onClose={onClose}>
            <div style={styles.content}>
                <h2 style={styles.title}>{title}</h2>
                <div style={styles.search}>
                    <IconSearch aria-hidden="true" style={styles.searchIcon} />
                    <input aria-label="絵文字を検索" autoFocus className={rules.input} onChange={(event) => setQuery(event.target.value)} placeholder="名前で検索" style={styles.input} type="search" value={query} />
                </div>
                <div style={styles.body}>
                    {customMatches.length > 0 && (
                        <section aria-label="カスタム絵文字" style={styles.section}>
                            <h3 style={styles.sectionTitle}>カスタム絵文字</h3>
                            <div style={styles.grid}>
                                {customMatches.map((emoji) => (
                                    <button aria-label={`:${emoji.name}:`} className={rules.cell} key={emoji.name} onClick={() => pick(`:${emoji.name}:`)} style={styles.cell} title={`:${emoji.name}:`} type="button">
                                        <CustomEmoji emoji={emoji} label="" style={styles.customImage} />
                                    </button>
                                ))}
                            </div>
                        </section>
                    )}
                    {unicodeMatches.length > 0 && (
                        <section aria-label="Unicode絵文字" style={styles.section}>
                            <h3 style={styles.sectionTitle}>Unicode絵文字</h3>
                            <div style={styles.grid}>
                                {unicodeMatches.map((emoji) => (
                                    <button aria-label={emoji.value} className={rules.cell} key={emoji.value} onClick={() => pick(emoji.value)} style={styles.cell} title={emoji.label} type="button">
                                        {emoji.value}
                                    </button>
                                ))}
                            </div>
                        </section>
                    )}
                    {customMatches.length === 0 && unicodeMatches.length === 0 && <p style={styles.empty}>該当する絵文字がありません</p>}
                </div>
            </div>
        </Modal>
    );
}
