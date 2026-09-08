import type { CSSProperties, ReactNode } from "react";

import { css } from "../../lib/css";

const styles = {
    root: {
        display: "inline-flex",
        alignItems: "center",
        gap: "0.625rem",
        cursor: "pointer",
        userSelect: "none",
    },
    input: {
        position: "absolute",
        width: 1,
        height: 1,
        margin: -1,
        padding: 0,
        overflow: "hidden",
        clipPath: "inset(50%)",
        whiteSpace: "nowrap",
    },
    track: {
        position: "relative",
        width: "2.75rem",
        height: "1.5rem",
        flexShrink: 0,
        border: "1px solid var(--border)",
        borderRadius: "9999px",
        background: "var(--panel-muted)",
        boxShadow: "inset 0 1px 2px rgb(0 0 0 / 8%)",
        transition: "background-color 140ms ease, border-color 140ms ease, box-shadow 140ms ease",
    },
    trackChecked: {
        borderColor: "var(--accent-hover)",
        background: "var(--accent)",
    },
    thumb: {
        position: "absolute",
        top: "0.1875rem",
        left: "0.1875rem",
        width: "1rem",
        height: "1rem",
        borderRadius: "9999px",
        background: "var(--muted)",
        boxShadow: "0 1px 3px rgb(0 0 0 / 25%)",
        transition: "transform 140ms cubic-bezier(.2,.8,.2,1), background-color 140ms ease",
    },
    thumbChecked: {
        background: "var(--accent-ink)",
        transform: "translateX(1.25rem)",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    root: css({
        '&[data-disabled="true"]': {
            cursor: "not-allowed",
            opacity: 0.55,
        },
        "& input:focus-visible + span": {
            outline: "3px solid var(--accent)",
            outlineOffset: "2px",
        },
    }),
};

export function Switch({ checked, disabled = false, label, onChange, style }: { checked: boolean; disabled?: boolean; label: ReactNode; onChange: (checked: boolean) => void; style?: CSSProperties }) {
    return (
        <label className={rules.root} data-disabled={disabled} style={{ ...styles.root, ...style }}>
            <input aria-checked={checked} checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} role="switch" style={styles.input} type="checkbox" />
            <span aria-hidden="true" style={{ ...styles.track, ...(checked ? styles.trackChecked : {}) }}>
                <span style={{ ...styles.thumb, ...(checked ? styles.thumbChecked : {}) }} />
            </span>
            <span>{label}</span>
        </label>
    );
}
