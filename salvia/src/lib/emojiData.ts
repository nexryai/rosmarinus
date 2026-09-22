import type { CompactEmoji } from "emojibase";
import compactEmojis from "emojibase-data/en/compact.json";

export type UnicodeEmoji = {
    value: string;
    label: string;
    searchText: string;
};

// Match Misskey's reaction normalization: drop the emoji variation selector
// unless the sequence is a ZWJ ligature, and keep the font fallback rendering
// the base codepoint in color.
const toReactionValue = (unicode: string) => (unicode.includes("\u200d") ? unicode : unicode.replace(/\ufe0f/g, ""));

const catalog = (compactEmojis as CompactEmoji[]).map(
    (emoji): UnicodeEmoji => ({
        value: toReactionValue(emoji.unicode),
        label: emoji.label,
        searchText: [emoji.label, ...(emoji.tags ?? [])].join(" ").toLowerCase(),
    }),
);

export const unicodeEmojis = catalog;

const byValue = new Map(catalog.map((emoji) => [emoji.value, emoji]));

const defaultUnicodeEmojiValues = ["👍", "❤", "😂", "🎉", "😮", "😢", "🙏", "🔥"];

export const defaultUnicodeEmojis = defaultUnicodeEmojiValues.flatMap((value) => {
    const emoji = byValue.get(value);
    return emoji ? [emoji] : [];
});

export const searchUnicodeEmojis = (query: string, limit = 100): UnicodeEmoji[] => {
    const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (terms.length === 0) return [];

    const matches: UnicodeEmoji[] = [];
    for (const emoji of catalog) {
        if (terms.every((term) => emoji.searchText.includes(term))) {
            matches.push(emoji);
            if (matches.length >= limit) break;
        }
    }
    return matches;
};
