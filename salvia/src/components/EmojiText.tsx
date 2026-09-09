import type { CSSProperties, ReactNode } from "react";

import { parseSimple } from "mfm-js";

import type { Emoji } from "../lib/schema";
import { Twemoji } from "./Twemoji";

const styles = {
    emoji: {
        width: "1.5em",
        height: "1.5em",
        marginInline: "0.1em",
        display: "inline-block",
        objectFit: "contain",
        verticalAlign: "-0.35em",
    },
} satisfies Record<string, CSSProperties>;

export function CustomEmoji({ emoji, label, style }: { emoji: Emoji; label?: string; style?: CSSProperties }) {
    return <img alt={label ?? `:${emoji.name}:`} draggable={false} loading="lazy" referrerPolicy="no-referrer" src={emoji.url} style={{ ...styles.emoji, ...style }} title={`:${emoji.name}:`} />;
}

export function EmojiText({ emojis = [], text }: { emojis?: Emoji[]; text: string }) {
    const byName = new Map(emojis.map((emoji) => [emoji.name, emoji]));
    let nodes: ReturnType<typeof parseSimple>;
    try {
        nodes = parseSimple(text);
    } catch {
        return <Twemoji text={text} />;
    }
    let keyOffset = 0;
    return (
        <>
            {nodes.map((node): ReactNode => {
                const key = `${node.type}-${keyOffset}`;
                keyOffset += JSON.stringify(node).length;
                if (node.type === "text") return <Twemoji key={key} text={node.props.text} />;
                if (node.type === "unicodeEmoji") return <Twemoji key={key} text={node.props.emoji} />;
                if (node.type === "plain") return <Twemoji key={key} text={node.children.map((child) => child.props.text).join("")} />;
                const emoji = byName.get(node.props.name);
                return emoji ? <CustomEmoji emoji={emoji} key={key} /> : <span key={key}>:{node.props.name}:</span>;
            })}
        </>
    );
}
