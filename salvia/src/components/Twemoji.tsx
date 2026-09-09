import type { CSSProperties, ReactNode } from "react";

import { parse } from "@twemoji/parser";

const TWEMOJI_VERSION = "v17.0.3";
const TWEMOJI_ASSET_BASE = `https://cdn.jsdelivr.net/gh/jdecked/twemoji@${TWEMOJI_VERSION}/assets/svg`;

const styles = {
    emoji: {
        width: "1em",
        height: "1em",
        marginInline: "0.075em",
        display: "inline-block",
        verticalAlign: "-0.1em",
    },
} satisfies Record<string, CSSProperties>;

const buildUrl = (codepoints: string) => `${TWEMOJI_ASSET_BASE}/${codepoints}.svg`;

export function Twemoji({ style, text }: { style?: CSSProperties; text: string }) {
    const entities = parse(text, { assetType: "svg", buildUrl });
    if (entities.length === 0) return <>{text}</>;

    const nodes: ReactNode[] = [];
    let offset = 0;
    for (const entity of entities) {
        const [start, end] = entity.indices;
        if (start > offset) nodes.push(text.slice(offset, start));
        nodes.push(<img alt={entity.text} draggable={false} key={`${start}-${entity.url}`} loading="lazy" referrerPolicy="no-referrer" src={entity.url} style={{ ...styles.emoji, ...style }} />);
        offset = end;
    }
    if (offset < text.length) nodes.push(text.slice(offset));

    return <>{nodes}</>;
}
