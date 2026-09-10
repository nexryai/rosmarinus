import type { CSSProperties, ReactNode } from "react";

import { type MfmNode, parse } from "mfm-js";

import { emojiFontFamily } from "../globalStyles";
import { css, keyframes } from "../lib/css";
import type { Emoji } from "../lib/schema";
import { CustomEmoji } from "./EmojiText";

const monospaceFontFamily = `SFMono-Regular, Consolas, "Liberation Mono", "DejaVu Sans Mono", ${emojiFontFamily}, ui-monospace, monospace`;

const spin = keyframes({
    to: {
        transform: "rotate(360deg)",
    },
});

const shake = keyframes({
    "0%": {
        transform: "translateX(0)",
    },
    "25%": {
        transform: "translateX(-0.12em)",
    },
    "75%": {
        transform: "translateX(0.12em)",
    },
    "100%": {
        transform: "translateX(0)",
    },
});

const jump = keyframes({
    "0%": {
        transform: "translateY(0)",
    },
    "50%": {
        transform: "translateY(-0.35em)",
    },
    "100%": {
        transform: "translateY(0)",
    },
});

const rainbow = keyframes({
    from: {
        filter: "hue-rotate(0deg)",
    },
    to: {
        filter: "hue-rotate(360deg)",
    },
});

const styles = {
    root: {
        overflowWrap: "break-word",
        whiteSpace: "pre-wrap",
    },
    quote: {
        marginBlock: "0.5rem",
        padding: "0.25rem 0 0.25rem 0.75rem",
        display: "block",
        borderLeft: "3px solid var(--muted)",
        opacity: 0.75,
    },
    center: {
        display: "block",
        textAlign: "center",
    },
    codeBlock: {
        marginBlock: "0.5rem",
        padding: "0.75rem",
        display: "block",
        overflowX: "auto",
        borderRadius: "0.75rem",
        background: "var(--panel-muted)",
        fontFamily: monospaceFontFamily,
        fontSize: "0.875em",
        whiteSpace: "pre",
    },
    inlineCode: {
        padding: "0.1em 0.3em",
        borderRadius: "0.3em",
        background: "var(--panel-muted)",
        fontFamily: monospaceFontFamily,
        fontSize: "0.9em",
    },
    link: {
        color: "var(--accent-hover)",
        textDecoration: "underline",
        textDecorationColor: "var(--accent-soft)",
        textUnderlineOffset: "0.15em",
    },
    mention: {
        color: "var(--accent-hover)",
        fontWeight: 700,
    },
    hashtag: {
        color: "var(--accent-hover)",
    },
    small: {
        opacity: 0.7,
    },
    function: {
        display: "inline-block",
        transformOrigin: "center",
    },
    search: {
        marginTop: "0.5rem",
        display: "inline-flex",
        gap: "0.35rem",
        alignItems: "center",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    blur: css({
        filter: "blur(0.35em)",
        transition: "filter 160ms ease",
        "&:hover": {
            filter: "blur(0)",
        },
    }),
    animated: css({
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
            animationIterationCount: 1,
        },
    }),
};

const safeURL = (value: string) => {
    try {
        const url = new URL(value);
        return url.protocol === "http:" || url.protocol === "https:" ? url.href : undefined;
    } catch {
        return undefined;
    }
};

const numericArgument = (value: string | true | undefined, fallback: number, minimum: number, maximum: number) => {
    if (typeof value !== "string") return fallback;
    const parsed = Number.parseFloat(value);
    return Number.isFinite(parsed) ? Math.min(maximum, Math.max(minimum, parsed)) : fallback;
};

const colorArgument = (value: string | true | undefined) => (typeof value === "string" && /^[0-9a-f]{3}(?:[0-9a-f]{3})?$/i.test(value) ? `#${value}` : undefined);
const speedArgument = (value: string | true | undefined, fallback: string) => (typeof value === "string" && /^(?:0|[0-9]+(?:\.[0-9]+)?)s$/.test(value) ? value : fallback);

const functionStyle = (node: Extract<MfmNode, { type: "fn" }>): CSSProperties | undefined => {
    const args = node.props.args;
    switch (node.props.name) {
        case "tada":
            return { ...styles.function, fontSize: "1.5em", animation: `${jump} ${speedArgument(args.speed, "0.8s")} ease-in-out infinite` };
        case "jelly":
        case "twitch":
        case "shake":
            return { ...styles.function, animation: `${shake} ${speedArgument(args.speed, "0.5s")} ease-in-out infinite` };
        case "jump":
        case "bounce":
            return { ...styles.function, animation: `${jump} ${speedArgument(args.speed, "0.75s")} ease-in-out infinite` };
        case "spin":
            return { ...styles.function, animation: `${spin} ${speedArgument(args.speed, "1.5s")} linear infinite`, animationDirection: args.alternate ? "alternate" : args.left ? "reverse" : "normal" };
        case "rainbow":
            return { ...styles.function, animation: `${rainbow} ${speedArgument(args.speed, "1s")} linear infinite` };
        case "flip":
            return { ...styles.function, transform: args.h && args.v ? "scale(-1, -1)" : args.v ? "scaleY(-1)" : "scaleX(-1)" };
        case "rotate":
            return { ...styles.function, transform: `rotate(${numericArgument(args.deg, 90, -3600, 3600)}deg)` };
        case "position":
            return { ...styles.function, transform: `translate(${numericArgument(args.x, 0, -5, 5)}em, ${numericArgument(args.y, 0, -5, 5)}em)` };
        case "scale":
            return { ...styles.function, transform: `scale(${numericArgument(args.x, 1, -5, 5)}, ${numericArgument(args.y, 1, -5, 5)})` };
        case "x2":
            return { ...styles.function, fontSize: "2em" };
        case "x3":
            return { ...styles.function, fontSize: "3em" };
        case "x4":
            return { ...styles.function, fontSize: "4em" };
        case "font":
            if (args.emoji) return { ...styles.function, fontFamily: emojiFontFamily };
            if (args.serif) return { ...styles.function, fontFamily: `Georgia, "Times New Roman", ${emojiFontFamily}, serif` };
            if (args.monospace) return { ...styles.function, fontFamily: monospaceFontFamily };
            if (args.cursive) return { ...styles.function, fontFamily: `"Comic Sans MS", "Brush Script MT", ${emojiFontFamily}, cursive` };
            if (args.fantasy) return { ...styles.function, fontFamily: `Impact, ${emojiFontFamily}, fantasy` };
            return styles.function;
        case "fg":
            return { ...styles.function, color: colorArgument(args.color) ?? "#f00" };
        case "bg":
            return { ...styles.function, backgroundColor: colorArgument(args.color) ?? "#f00" };
        case "border": {
            const borderStyle = typeof args.style === "string" && ["hidden", "dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset"].includes(args.style) ? args.style : "solid";
            return {
                ...styles.function,
                paddingInline: "0.15em",
                borderColor: colorArgument(args.color) ?? "var(--accent-hover)",
                borderStyle,
                borderWidth: numericArgument(args.width, 1, 0, 10),
                borderRadius: numericArgument(args.radius, 0, 0, 100),
            };
        }
        default:
            return undefined;
    }
};

const renderNodes = (nodes: MfmNode[], emojis: Map<string, Emoji>, path = "mfm"): ReactNode[] =>
    nodes.map((node, index) => {
        const key = `${path}-${index}`;
        const children = "children" in node && node.children ? renderNodes(node.children, emojis, key) : undefined;
        switch (node.type) {
            case "text":
                return node.props.text;
            case "unicodeEmoji":
                return node.props.emoji;
            case "emojiCode": {
                const emoji = emojis.get(node.props.name);
                return emoji ? <CustomEmoji emoji={emoji} key={key} /> : <span key={key}>:{node.props.name}:</span>;
            }
            case "bold":
                return <strong key={key}>{children}</strong>;
            case "small":
                return (
                    <small key={key} style={styles.small}>
                        {children}
                    </small>
                );
            case "italic":
                return <i key={key}>{children}</i>;
            case "strike":
                return <s key={key}>{children}</s>;
            case "inlineCode":
                return (
                    <code key={key} style={styles.inlineCode}>
                        {node.props.code}
                    </code>
                );
            case "blockCode":
                return (
                    <code data-language={node.props.lang || undefined} key={key} style={styles.codeBlock}>
                        {node.props.code}
                    </code>
                );
            case "mathInline":
                return (
                    <code key={key} style={styles.inlineCode}>
                        {node.props.formula}
                    </code>
                );
            case "mathBlock":
                return (
                    <code key={key} style={styles.codeBlock}>
                        {node.props.formula}
                    </code>
                );
            case "quote":
                return (
                    <span key={key} style={styles.quote}>
                        {children}
                    </span>
                );
            case "center":
                return (
                    <span key={key} style={styles.center}>
                        {children}
                    </span>
                );
            case "mention":
                return (
                    <span key={key} style={styles.mention}>
                        @{node.props.acct}
                    </span>
                );
            case "hashtag":
                return (
                    <span key={key} style={styles.hashtag}>
                        #{node.props.hashtag}
                    </span>
                );
            case "url": {
                const url = safeURL(node.props.url);
                return url ? (
                    <a href={url} key={key} rel="nofollow noreferrer noopener ugc" style={styles.link} target="_blank">
                        {node.props.url}
                    </a>
                ) : (
                    <span key={key}>{node.props.url}</span>
                );
            }
            case "link": {
                const url = safeURL(node.props.url);
                return url ? (
                    <a href={url} key={key} rel="nofollow noreferrer noopener ugc" style={styles.link} target="_blank">
                        {children}
                    </a>
                ) : (
                    <span key={key}>{children}</span>
                );
            }
            case "plain":
                return <span key={key}>{children}</span>;
            case "search": {
                const url = `https://www.google.com/search?q=${encodeURIComponent(node.props.query)}`;
                return (
                    <a href={url} key={key} rel="nofollow noreferrer noopener" style={{ ...styles.link, ...styles.search }} target="_blank">
                        {node.props.content}
                    </a>
                );
            }
            case "fn": {
                if (node.props.name === "ruby") {
                    const text = node.children.length === 1 && node.children[0]?.type === "text" ? node.children[0].props.text : "";
                    const separator = text.indexOf(" ");
                    if (separator > 0)
                        return (
                            <ruby key={key}>
                                {text.slice(0, separator)}
                                <rt>{text.slice(separator + 1)}</rt>
                            </ruby>
                        );
                }
                if (node.props.name === "unixtime") {
                    const text = node.children[0]?.type === "text" ? node.children[0].props.text : "";
                    const timestamp = Number.parseInt(text, 10) * 1000;
                    const date = new Date(timestamp);
                    if (!Number.isNaN(date.getTime()))
                        return (
                            <time dateTime={date.toISOString()} key={key}>
                                {date.toLocaleString("ja")}
                            </time>
                        );
                }
                if (node.props.name === "blur")
                    return (
                        <span className={rules.blur} key={key}>
                            {children}
                        </span>
                    );
                if (node.props.name === "sparkle" || node.props.name === "clickable") return <span key={key}>{children}</span>;
                const style = functionStyle(node);
                return style ? (
                    <span className={rules.animated} key={key} style={style}>
                        {children}
                    </span>
                ) : (
                    <span key={key}>
                        $[{node.props.name} {children}]
                    </span>
                );
            }
            default:
                return null;
        }
    });

export function Mfm({ className = "", emojis = [], style, text }: { className?: string; emojis?: Emoji[]; style?: CSSProperties; text: string }) {
    const byName = new Map(emojis.map((emoji) => [emoji.name, emoji]));
    let nodes: MfmNode[];
    try {
        nodes = parse(text, { nestLimit: 20 });
    } catch {
        return (
            <span className={className} style={{ ...styles.root, ...style }}>
                {text}
            </span>
        );
    }
    return (
        <span className={className} style={{ ...styles.root, ...style }}>
            {renderNodes(nodes, byName)}
        </span>
    );
}
