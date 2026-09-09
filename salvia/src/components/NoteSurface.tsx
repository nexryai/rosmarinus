import type { CSSProperties, HTMLAttributes } from "react";

import { css } from "../lib/css";

const styles = {
    surface: {
        display: "grid",
        gridTemplateColumns: "auto minmax(0, 1fr)",
        columnGap: "0.75rem",
        rowGap: "0.5rem",
        transition: "background-color 150ms, border-color 150ms",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    surface: css({
        paddingInline: "1.25rem",
        paddingBlock: "1.25rem",
        "&:hover": {
            background: "var(--panel-muted)",
        },
        "@media (width >= 40rem)": {
            paddingInline: "1.75rem",
            width: "calc(100% - 1.5rem)",
            maxWidth: "64rem",
            marginBlock: "0.75rem",
            marginInline: "auto",
            overflow: "clip",
            border: "1px solid var(--border)",
            borderRadius: "1.25rem",
            background: "var(--panel)",
        },
        ':root[data-compact="true"] &': {
            paddingBlock: "0.75rem",
        },
    }),
};

export function NoteSurface({ className = "", style, ...props }: HTMLAttributes<HTMLElement>) {
    return <article className={[rules.surface, className].filter(Boolean).join(" ")} style={{ ...styles.surface, ...style }} {...props} />;
}
