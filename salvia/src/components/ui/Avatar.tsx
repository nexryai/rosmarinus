import type { CSSProperties } from "react";

import { RippleButton } from "@/components/ui/RippleButton";
import type { Actor } from "@/lib/schema";

const styles = {
    avatar: {
        objectFit: "cover",
        boxShadow: "0 1px 3px #0000001a, 0 1px 2px -1px #0000001a",
        background: "var(--accent-soft)",
        borderRadius: "9999px",
        flexShrink: 0,
    },
    fallback: {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        fontWeight: 900,
        color: "var(--accent-ink)",
        background: "linear-gradient(135deg, #f8d56a, var(--accent-hover))",
    },
    button: {
        padding: 0,
        display: "block",
        overflow: "hidden",
        flexShrink: 0,
        borderRadius: "9999px",
    },
} satisfies Record<string, CSSProperties>;

const sizes = {
    xsmall: {
        width: "1.75rem",
        height: "1.75rem",
        fontSize: "0.6875rem",
        lineHeight: 1.273,
    },
    small: {
        width: "2.25rem",
        height: "2.25rem",
        fontSize: "0.75rem",
        lineHeight: 1.333,
    },
    medium: {
        width: "2.75rem",
        height: "2.75rem",
        fontSize: "0.875rem",
        lineHeight: 1.429,
    },
    large: {
        width: "6rem",
        height: "6rem",
        fontSize: "1.5rem",
        lineHeight: 1.333,
        boxShadow: "0 0 0 4px var(--panel), 0 1px 3px #0000001a",
    },
} satisfies Record<string, CSSProperties>;

type AvatarActor = Pick<Actor, "id" | "avatar_url" | "name" | "username">;
type AvatarSize = "xsmall" | "small" | "medium" | "large";

export function Avatar({ actor, onOpenProfile, size = "medium" }: { actor?: AvatarActor; onOpenProfile?: (actorID: string) => void; size?: AvatarSize }) {
    const label = actor?.name || actor?.username || "?";
    const content = actor?.avatar_url ? (
        <img alt={`${label}のアバター`} loading="lazy" referrerPolicy="no-referrer" src={actor.avatar_url} style={{ ...styles.avatar, ...sizes[size] }} />
    ) : (
        <span aria-label={`${label}のアバター`} role="img" style={{ ...styles.avatar, ...styles.fallback, ...sizes[size] }}>
            {label.slice(0, 1).toUpperCase()}
        </span>
    );
    if (!actor || !onOpenProfile) return content;
    return (
        <RippleButton aria-label={`${label}のプロフィールを開く`} onClick={() => onOpenProfile(actor.id)} style={{ ...styles.button, ...sizes[size] }} type="button">
            {content}
        </RippleButton>
    );
}
