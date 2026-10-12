import { type ButtonHTMLAttributes, Component, type CSSProperties, type ErrorInfo, type HTMLAttributes, type PropsWithChildren, type ReactNode, useCallback, useEffect, useRef, useState } from "react";

import { IconAlertCircle, IconLoader2, IconX } from "@tabler/icons-react";
import { css, keyframes } from "mintcss";

import { DrawerFrame } from "@/components/ui/Drawer";
import { RippleButton } from "@/components/ui/RippleButton";
import { useIsMobile } from "@/lib/responsive";

export { Avatar } from "@/components/ui/Avatar";
export { Tabs } from "@/components/ui/Tabs";

const spin = keyframes({
    to: {
        transform: "rotate(360deg)",
    },
});
const modalBackdropOpen = keyframes({
    from: {
        opacity: 0,
    },
    to: {
        opacity: 1,
    },
});
const modalBackdropClose = keyframes({
    from: {
        opacity: 1,
    },
    to: {
        opacity: 0,
    },
});
const modalOpen = keyframes({
    from: {
        opacity: 0,
        transform: "translate3d(0, 0.625rem, 0) scale(0.98)",
    },
    to: {
        opacity: 1,
        transform: "translate3d(0, 0, 0) scale(1)",
    },
});
const modalClose = keyframes({
    from: {
        opacity: 1,
        transform: "translate3d(0, 0, 0) scale(1)",
    },
    to: {
        opacity: 0,
        transform: "translate3d(0, 0.375rem, 0) scale(0.985)",
    },
});

const modalCloseDuration = 110;
const motionIsReduced = () => document.documentElement.dataset.reduceMotion === "true" || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;

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
    fatalTitle: {
        fontSize: "1.5rem",
        lineHeight: 1.333,
        fontWeight: 900,
    },
    button: {
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "0.5rem",
        overflow: "clip",
        position: "relative",
        border: 0,
        borderRadius: "9999px",
        fontWeight: 700,
        transition: "color 150ms, background-color 150ms",
        userSelect: "none",
    },
    buttonContent: {
        position: "relative",
        zIndex: 1,
        pointerEvents: "none",
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "0.5rem",
        whiteSpace: "nowrap",
    },
    stateMessage: {
        minHeight: "13rem",
        paddingInline: "1.5rem",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "0.5rem",
        color: "var(--muted)",
        fontSize: "0.875rem",
        lineHeight: 1.429,
    },
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
    modalBackdrop: {
        position: "fixed",
        zIndex: 50,
        inset: 0,
        padding: "1rem",
        display: "grid",
        placeItems: "center",
        backgroundColor: "rgb(0 0 0 / 40%)",
        backdropFilter: "blur(8px)",
        willChange: "opacity",
    },
    modal: {
        position: "relative",
        width: "100%",
        maxWidth: "36rem",
        border: "1px solid var(--border)",
        borderRadius: "1.5rem",
        color: "var(--text)",
        background: "var(--panel)",
        boxShadow: "0 25px 50px -12px #00000040",
        transformOrigin: "center",
        willChange: "transform, opacity",
    },
    modalClose: {
        position: "absolute",
        top: "1rem",
        right: "1rem",
        width: "2.25rem",
        height: "2.25rem",
        display: "grid",
        placeItems: "center",
        borderRadius: "9999px",
    },
    pageHeader: {
        position: "sticky",
        zIndex: 20,
        top: "var(--shell-header-height, 0px)",
        height: "3.5rem",
        alignItems: "center",
        borderBottom: "1px solid var(--border)",
        backdropFilter: "blur(24px)",
    },
    eyebrow: {
        marginBottom: "0.125rem",
        color: "var(--muted)",
        fontSize: "11px",
        fontWeight: 700,
        letterSpacing: "0.1em",
        textTransform: "uppercase",
    },
    pageTitle: {
        margin: 0,
        fontSize: "1rem",
        lineHeight: 1.4,
        fontWeight: 700,
        letterSpacing: "-0.025em",
    },
    roundButton: {
        width: "2.5rem",
        height: "2.5rem",
        marginLeft: "auto",
        display: "grid",
        placeItems: "center",
        borderRadius: "9999px",
        transition: "color 150ms, background-color 150ms",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    button: css({
        "& > svg": {
            width: "1.25rem",
            height: "1.25rem",
        },
    }),
    primary: css({
        color: "var(--accent-ink)",
        background: "var(--accent)",
        "&:hover": {
            background: "var(--accent-hover)",
        },
    }),
    secondary: css({
        color: "var(--accent-ink)",
        background: "var(--accent-soft)",
    }),
    ghost: css({
        color: "var(--muted)",
        background: "transparent",
        "&:hover": {
            color: "var(--text)",
            background: "var(--panel-muted)",
        },
    }),
    danger: css({
        color: "#fff",
        background: "var(--danger)",
    }),
    stateMessage: css({
        "& > svg": {
            width: "1.25rem",
            height: "1.25rem",
        },
    }),
    spin: css({
        animation: `${spin} 850ms linear infinite`,
    }),
    errorBanner: css({
        borderColor: "var(--danger)",
        background: "var(--danger)",
        "@supports (color: color-mix(in lab, red, red))": {
            borderColor: "color-mix(in srgb, var(--danger) 25%, transparent)",
            background: "color-mix(in srgb, var(--danger) 7%, var(--panel))",
        },
        "& > svg": {
            width: "1.25rem",
            height: "1.25rem",
            flexShrink: 0,
        },
        "& > button": {
            marginLeft: "auto",
            background: "transparent",
        },
        "& > button > svg": {
            width: "1rem",
            height: "1rem",
        },
    }),
    modalBackdrop: css({
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
        },
    }),
    modal: css({
        padding: "1.25rem",
        "@media (width >= 40rem)": {
            padding: "1.5rem",
        },
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
        },
    }),
    modalClose: css({
        color: "var(--muted)",
        background: "transparent",
        "&:hover": {
            background: "var(--panel-muted)",
        },
        "& > svg": {
            width: "1.25rem",
            height: "1.25rem",
        },
    }),
    pageHeader: css({
        display: "none",
        paddingInline: "1.25rem",
        background: "var(--panel)",
        "@supports (color: color-mix(in lab, red, red))": {
            background: "color-mix(in srgb, var(--panel) 88%, transparent)",
        },
        "@media (width >= 40rem)": {
            paddingInline: "1.75rem",
        },
        "@media (width >= 64rem)": {
            display: "flex",
        },
    }),
    roundButton: css({
        color: "var(--muted)",
        "&:hover": {
            color: "var(--text)",
            background: "var(--panel-muted)",
        },
        "& > svg": {
            width: "1.25rem",
            height: "1.25rem",
        },
    }),
    dividedList: css({
        "& > :not(:last-child)": {
            borderBottom: "1px solid var(--border)",
        },
    }),
};

type ButtonSize = "small" | "medium" | "large";

const buttonSizes = {
    small: {
        minHeight: "2.25rem",
        paddingInline: "0.875rem",
        fontSize: "0.875rem",
    },
    medium: {
        minHeight: "2.5rem",
        paddingInline: "1.25rem",
    },
    large: {
        minHeight: "3rem",
        paddingInline: "1.5rem",
        fontSize: "1rem",
    },
} satisfies Record<ButtonSize, CSSProperties>;

export function PageHeader({ leading, title, trailing }: { leading?: ReactNode; title: string; trailing?: ReactNode }) {
    return (
        <header className={rules.pageHeader} style={styles.pageHeader}>
            {leading}
            <div>
                <h1 style={styles.pageTitle}>{title}</h1>
            </div>
            {trailing}
        </header>
    );
}

export function RoundButton({ style, ...props }: ButtonHTMLAttributes<HTMLButtonElement>) {
    return <RippleButton className={rules.roundButton} style={{ ...styles.roundButton, ...style }} type="button" {...props} />;
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
        console.error("Rosemary render failed", error, info.componentStack);
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

export function Button({ children, className = "", size = "small", style, type = "button", variant = "primary", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { disableRipple?: boolean; size?: ButtonSize; variant?: "primary" | "secondary" | "ghost" | "danger" }) {
    return (
        <RippleButton className={`${rules.button} ${rules[variant]} ${className}`} style={{ ...styles.button, ...buttonSizes[size], ...style }} type={type} {...props}>
            <span style={styles.buttonContent}>{children}</span>
        </RippleButton>
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
                <RippleButton aria-label="閉じる" onClick={onDismiss} type="button">
                    <IconX />
                </RippleButton>
            )}
        </div>
    );
}

export function Modal({ children, dismissible = true, label, onClose }: PropsWithChildren<{ dismissible?: boolean; label: string; onClose: () => void }>) {
    const [phase, setPhase] = useState<"open" | "closing">("open");
    const closeTimer = useRef<number | undefined>(undefined);
    const isMobile = useIsMobile();

    const requestClose = useCallback(() => {
        if (phase === "closing") return;
        if (motionIsReduced()) {
            onClose();
            return;
        }
        setPhase("closing");
        closeTimer.current = window.setTimeout(onClose, modalCloseDuration);
    }, [onClose, phase]);

    useEffect(() => {
        if (isMobile || !dismissible) return;
        const onKeyDown = (event: globalThis.KeyboardEvent) => {
            if (event.key !== "Escape") return;
            event.preventDefault();
            requestClose();
        };
        document.addEventListener("keydown", onKeyDown);
        return () => document.removeEventListener("keydown", onKeyDown);
    }, [dismissible, isMobile, requestClose]);

    useEffect(
        () => () => {
            if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
        },
        [],
    );

    const content = (
        <>
            <RippleButton aria-label="閉じる" className={rules.modalClose} onClick={requestClose} style={styles.modalClose} type="button">
                <IconX />
            </RippleButton>
            {children}
        </>
    );

    if (isMobile) {
        return (
            <DrawerFrame dismissible={dismissible} label={label} onDismiss={requestClose} phase={phase}>
                {content}
            </DrawerFrame>
        );
    }

    return (
        <div
            aria-hidden={phase === "closing" ? true : undefined}
            className={rules.modalBackdrop}
            onMouseDown={(event) => {
                if (dismissible && event.currentTarget === event.target) requestClose();
            }}
            role="presentation"
            style={{
                ...styles.modalBackdrop,
                animation: `${phase === "open" ? modalBackdropOpen : modalBackdropClose} ${phase === "open" ? 140 : modalCloseDuration}ms cubic-bezier(.2,.8,.2,1) both`,
                pointerEvents: phase === "closing" ? "none" : "auto",
            }}
        >
            <section
                aria-label={label}
                aria-modal="true"
                className={rules.modal}
                role="dialog"
                style={{
                    ...styles.modal,
                    animation: `${phase === "open" ? modalOpen : modalClose} ${phase === "open" ? 160 : modalCloseDuration}ms cubic-bezier(.2,.8,.2,1) both`,
                }}
            >
                {content}
            </section>
        </div>
    );
}
