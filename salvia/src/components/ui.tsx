import { type ButtonHTMLAttributes, Component, type CSSProperties, type ErrorInfo, type HTMLAttributes, type MouseEvent, type PropsWithChildren, type ReactNode, useEffect, useRef, useState } from "react";

import { IconAlertCircle, IconLoader2, IconX } from "@tabler/icons-react";

import { css, keyframes } from "../lib/css";
import type { Actor } from "../lib/schema";

const spin = keyframes({ to: { transform: "rotate(360deg)" } });
const rippleAnimation = keyframes({ to: { boxShadow: "0 0 0 var(--ripple-radius) transparent" } });

const styles = {
    fatalPage: {
        minHeight: "100dvh",
        maxWidth: "28rem",
        marginInline: "auto",
        padding: "2.5rem 1.25rem",
        display: "flex",
        flexDirection: "column",
        placeItems: "center",
        justifyContent: "center",
        gap: "1rem",
        textAlign: "center",
        background: "radial-gradient(circle at 50% 0, var(--accent-soft), transparent 38%), var(--page)",
    },
    fatalTitle: { fontSize: "1.5rem", lineHeight: 1.333, fontWeight: 900 },
    button: {
        minHeight: "2.5rem",
        paddingInline: "1.25rem",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "0.5rem",
        overflow: "clip",
        position: "relative",
        border: 0,
        borderRadius: "9999px",
        fontWeight: 700,
        transition: "all 150ms cubic-bezier(.4, 0, .2, 1)",
        userSelect: "none",
    },
    buttonContent: { position: "relative", zIndex: 1, pointerEvents: "none", display: "inline-flex", alignItems: "center", justifyContent: "center", gap: "0.5rem" },
    ripple: {
        position: "absolute",
        zIndex: 0,
        width: 2,
        height: 2,
        borderRadius: "9999px",
        pointerEvents: "none",
        background: "transparent",
        boxShadow: "0 0 0 0 rgb(0 0 0 / 10%)",
        animation: `${rippleAnimation} 500ms cubic-bezier(0,.5,0,1) forwards`,
    },
    avatar: { objectFit: "cover", boxShadow: "0 1px 3px #0000001a, 0 1px 2px -1px #0000001a", background: "var(--accent-soft)", borderRadius: "9999px", flexShrink: 0 },
    avatarFallback: { display: "inline-flex", alignItems: "center", justifyContent: "center", fontWeight: 900, color: "var(--accent-ink)", background: "linear-gradient(135deg, #f8d56a, var(--accent-hover))" },
    stateMessage: { minHeight: "13rem", paddingInline: "1.5rem", display: "flex", alignItems: "center", justifyContent: "center", gap: "0.5rem", color: "var(--muted)", fontSize: "0.875rem", lineHeight: 1.429 },
    errorBanner: {
        margin: "1rem",
        padding: "0.75rem 1rem",
        display: "flex",
        alignItems: "center",
        gap: "0.5rem",
        borderWidth: 1,
        borderStyle: "solid",
        borderRadius: "1rem",
        color: "var(--danger)",
        fontSize: "0.875rem",
    },
    modalBackdrop: { position: "fixed", zIndex: 50, inset: 0, padding: "1rem", display: "grid", placeItems: "center", backgroundColor: "rgb(0 0 0 / 40%)", backdropFilter: "blur(8px)" },
    modal: { position: "relative", width: "100%", maxWidth: "36rem", border: "1px solid var(--border)", borderRadius: "1.5rem", color: "var(--text)", background: "var(--panel)", boxShadow: "0 25px 50px -12px #00000040" },
    modalClose: { position: "absolute", top: "1rem", right: "1rem", width: "2.25rem", height: "2.25rem", display: "grid", placeItems: "center", borderRadius: "9999px" },
    pageHeader: { position: "sticky", zIndex: 20, top: 0, height: "5rem", display: "flex", alignItems: "center", borderBottom: "1px solid var(--border)", backdropFilter: "blur(24px)" },
    eyebrow: { marginBottom: "0.125rem", color: "var(--muted)", fontSize: "11px", fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase" },
    pageTitle: { margin: 0, fontSize: "1.25rem", lineHeight: 1.4, fontWeight: 900, letterSpacing: "-0.025em" },
    roundButton: { width: "2.5rem", height: "2.5rem", marginLeft: "auto", display: "grid", placeItems: "center", borderRadius: "9999px", transition: "color 150ms, background-color 150ms" },
} satisfies Record<string, CSSProperties>;

const avatarSizes = {
    small: { width: "2.25rem", height: "2.25rem", fontSize: "0.75rem", lineHeight: 1.333 },
    medium: { width: "2.75rem", height: "2.75rem", fontSize: "0.875rem", lineHeight: 1.429 },
    large: { width: "6rem", height: "6rem", fontSize: "1.5rem", lineHeight: 1.333, boxShadow: "0 0 0 4px var(--panel), 0 1px 3px #0000001a" },
} satisfies Record<string, CSSProperties>;

const rules = {
    button: css({ "&:active": { scale: 0.98 }, "& > svg": { width: "1.25rem", height: "1.25rem" } }),
    primary: css({ color: "var(--accent-ink)", background: "var(--accent)", "&:hover": { background: "var(--accent-hover)" } }),
    secondary: css({ color: "var(--accent-ink)", background: "var(--accent-soft)" }),
    ghost: css({ color: "var(--muted)", background: "transparent", "&:hover": { color: "var(--text)", background: "var(--panel-muted)" } }),
    danger: css({ color: "#fff", background: "var(--danger)" }),
    stateMessage: css({ "& > svg": { width: "1.25rem", height: "1.25rem" } }),
    spin: css({ animation: `${spin} 850ms linear infinite` }),
    errorBanner: css({
        borderColor: "var(--danger)",
        background: "var(--danger)",
        "@supports (color: color-mix(in lab, red, red))": { borderColor: "color-mix(in srgb, var(--danger) 25%, transparent)", background: "color-mix(in srgb, var(--danger) 7%, var(--panel))" },
        "& > svg": { width: "1.25rem", height: "1.25rem", flexShrink: 0 },
        "& > button": { marginLeft: "auto", background: "transparent" },
        "& > button > svg": { width: "1rem", height: "1rem" },
    }),
    modal: css({ padding: "1.25rem", "@media (width >= 40rem)": { padding: "1.5rem" } }),
    modalClose: css({ color: "var(--muted)", background: "transparent", "&:hover": { background: "var(--panel-muted)" }, "& > svg": { width: "1.25rem", height: "1.25rem" } }),
    pageHeader: css({ paddingInline: "1.25rem", background: "var(--panel)", "@supports (color: color-mix(in lab, red, red))": { background: "color-mix(in srgb, var(--panel) 88%, transparent)" }, "@media (width >= 40rem)": { paddingInline: "1.75rem" } }),
    roundButton: css({ color: "var(--muted)", "&:hover": { color: "var(--text)", background: "var(--panel-muted)" }, "& > svg": { width: "1.25rem", height: "1.25rem" } }),
    dividedList: css({ "& > :not(:last-child)": { borderBottom: "1px solid var(--border)" } }),
};

export function PageHeader({ eyebrow, leading, title, trailing }: { eyebrow: string; leading?: ReactNode; title: string; trailing?: ReactNode }) {
    return (
        <header className={rules.pageHeader} style={styles.pageHeader}>
            {leading}
            <div>
                <p style={styles.eyebrow}>{eyebrow}</p>
                <h1 style={styles.pageTitle}>{title}</h1>
            </div>
            {trailing}
        </header>
    );
}

export function RoundButton({ style, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
    return <button className={rules.roundButton} style={{ ...styles.roundButton, ...style }} type="button" {...props} />;
}

export function DividedList({ children, className = "", ...props }: HTMLAttributes<HTMLElement>) {
    return (
        <section className={`${rules.dividedList} ${className}`} {...props}>
            {children}
        </section>
    );
}

export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
    state = { failed: false };

    static getDerivedStateFromError() {
        return { failed: true };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error("Salvia render failed", error, info.componentStack);
    }

    render() {
        if (!this.state.failed) return this.props.children;
        return (
            <main style={styles.fatalPage}>
                <h1 style={styles.fatalTitle}>画面を表示できません</h1>
                <p>再読み込みしてもう一度お試しください。</p>
                <Button onClick={() => window.location.reload()}>再読み込み</Button>
            </main>
        );
    }
}

type Ripple = { id: number; x: number; y: number; radius: number };

export function Button({ children, className = "", disableRipple = false, onMouseDown, style, type = "button", variant = "primary", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { disableRipple?: boolean; variant?: "primary" | "secondary" | "ghost" | "danger" }) {
    const [ripples, setRipples] = useState<Ripple[]>([]);
    const nextRippleID = useRef(0);
    const timers = useRef<number[]>([]);

    useEffect(
        () => () => {
            for (const timer of timers.current) window.clearTimeout(timer);
        },
        [],
    );

    const handleMouseDown = (event: MouseEvent<HTMLButtonElement>) => {
        if (!disableRipple && document.documentElement.dataset.reduceMotion !== "true") {
            const rect = event.currentTarget.getBoundingClientRect();
            const x = event.clientX - rect.left;
            const y = event.clientY - rect.top;
            const radius = Math.max(Math.hypot(x, y), Math.hypot(rect.width - x, y), Math.hypot(x, rect.height - y), Math.hypot(rect.width - x, rect.height - y));
            const id = nextRippleID.current++;
            setRipples((current) => [...current, { id, x, y, radius }]);
            timers.current.push(
                window.setTimeout(() => {
                    setRipples((current) => current.filter((ripple) => ripple.id !== id));
                }, 500),
            );
        }
        onMouseDown?.(event);
    };

    return (
        <button className={`${rules.button} ${rules[variant]} ${className}`} onMouseDown={handleMouseDown} style={{ ...styles.button, ...style }} type={type} {...props}>
            {ripples.map((ripple) => (
                <span aria-hidden="true" data-testid="button-ripple" key={ripple.id} style={{ ...styles.ripple, left: ripple.x - 1, top: ripple.y - 1, "--ripple-radius": `${ripple.radius}px` } as CSSProperties} />
            ))}
            <span style={styles.buttonContent}>{children}</span>
        </button>
    );
}

export function Avatar({ actor, size = "medium" }: { actor?: Pick<Actor, "avatar_url" | "name" | "username">; size?: "small" | "medium" | "large" }) {
    const label = actor?.name || actor?.username || "?";
    return actor?.avatar_url ? (
        <img alt={`${label}のアバター`} loading="lazy" referrerPolicy="no-referrer" src={actor.avatar_url} style={{ ...styles.avatar, ...avatarSizes[size] }} />
    ) : (
        <span aria-label={`${label}のアバター`} role="img" style={{ ...styles.avatar, ...styles.avatarFallback, ...avatarSizes[size] }}>
            {label.slice(0, 1).toUpperCase()}
        </span>
    );
}

export function Loading({ label = "読み込み中" }: { label?: string }) {
    return (
        <div className={rules.stateMessage} role="status" style={styles.stateMessage}>
            <IconLoader2 className={rules.spin} />
            {label}
        </div>
    );
}

export function Empty({ children }: PropsWithChildren) {
    return <div style={{ ...styles.stateMessage, textAlign: "center" }}>{children}</div>;
}

export function ErrorBanner({ message, onDismiss }: { message: string; onDismiss?: () => void }) {
    return (
        <div className={rules.errorBanner} role="alert" style={styles.errorBanner}>
            <IconAlertCircle />
            <span>{message}</span>
            {onDismiss && (
                <button aria-label="閉じる" onClick={onDismiss} type="button">
                    <IconX />
                </button>
            )}
        </div>
    );
}

export function Modal({ children, label, onClose }: PropsWithChildren<{ label: string; onClose: () => void }>) {
    return (
        <div
            onMouseDown={(event) => {
                if (event.currentTarget === event.target) onClose();
            }}
            role="presentation"
            style={styles.modalBackdrop}
        >
            <section aria-label={label} aria-modal="true" className={rules.modal} role="dialog" style={styles.modal}>
                <button aria-label="閉じる" className={rules.modalClose} onClick={onClose} style={styles.modalClose} type="button">
                    <IconX />
                </button>
                {children}
            </section>
        </div>
    );
}
