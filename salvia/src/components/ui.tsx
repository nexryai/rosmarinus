import { type ButtonHTMLAttributes, Component, type ErrorInfo, type PropsWithChildren, type ReactNode } from "react";

import { IconAlertCircle, IconLoader2, IconX } from "@tabler/icons-react";

import type { Actor } from "../lib/schema";

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
            <main className="fatal-page">
                <h1>画面を表示できません</h1>
                <p>再読み込みしてもう一度お試しください。</p>
                <Button onClick={() => window.location.reload()}>再読み込み</Button>
            </main>
        );
    }
}

export function Button({ className = "", variant = "primary", ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "ghost" | "danger" }) {
    return <button className={`button button--${variant} ${className}`} type="button" {...props} />;
}

export function Avatar({ actor, size = "medium" }: { actor?: Pick<Actor, "avatar_url" | "name" | "username">; size?: "small" | "medium" | "large" }) {
    const label = actor?.name || actor?.username || "?";
    return actor?.avatar_url ? (
        <img alt={`${label}のアバター`} className={`avatar avatar--${size}`} loading="lazy" referrerPolicy="no-referrer" src={actor.avatar_url} />
    ) : (
        <span aria-label={`${label}のアバター`} className={`avatar avatar--${size} avatar--fallback`} role="img">
            {label.slice(0, 1).toUpperCase()}
        </span>
    );
}

export function Loading({ label = "読み込み中" }: { label?: string }) {
    return (
        <div className="state-message" role="status">
            <IconLoader2 className="spin" />
            {label}
        </div>
    );
}

export function Empty({ children }: PropsWithChildren) {
    return <div className="state-message state-message--empty">{children}</div>;
}

export function ErrorBanner({ message, onDismiss }: { message: string; onDismiss?: () => void }) {
    return (
        <div className="error-banner" role="alert">
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
            className="modal-backdrop"
            onMouseDown={(event) => {
                if (event.currentTarget === event.target) onClose();
            }}
            role="presentation"
        >
            <section aria-label={label} aria-modal="true" className="modal" role="dialog">
                <button aria-label="閉じる" className="modal__close" onClick={onClose} type="button">
                    <IconX />
                </button>
                {children}
            </section>
        </div>
    );
}
