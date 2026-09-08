import { type CSSProperties, type PropsWithChildren, useEffect } from "react";
import { createPortal } from "react-dom";

import { css, keyframes } from "../../lib/css";

const backdropOpen = keyframes({
    from: {
        opacity: 0,
    },
    to: {
        opacity: 1,
    },
});

const backdropClose = keyframes({
    from: {
        opacity: 1,
    },
    to: {
        opacity: 0,
    },
});

const drawerOpen = keyframes({
    from: {
        transform: "translate3d(0, 100%, 0)",
    },
    to: {
        transform: "translate3d(0, 0, 0)",
    },
});

const drawerClose = keyframes({
    from: {
        transform: "translate3d(0, 0, 0)",
    },
    to: {
        transform: "translate3d(0, 100%, 0)",
    },
});

const styles = {
    backdrop: {
        position: "fixed",
        zIndex: 100,
        inset: 0,
        paddingTop: "3rem",
        display: "flex",
        alignItems: "flex-end",
        justifyContent: "center",
        background: "rgb(0 0 0 / 42%)",
        backdropFilter: "blur(5px)",
        overscrollBehavior: "contain",
        willChange: "opacity",
    },
    drawer: {
        position: "relative",
        width: "100%",
        maxWidth: "40rem",
        maxHeight: "calc(100dvh - 3rem)",
        padding: "0.5rem 1rem max(1rem, env(safe-area-inset-bottom))",
        overflowY: "auto",
        overscrollBehavior: "contain",
        border: "1px solid var(--border)",
        borderBottom: 0,
        borderRadius: "1.5rem 1.5rem 0 0",
        color: "var(--text)",
        background: "var(--panel)",
        boxShadow: "0 -18px 50px rgb(0 0 0 / 18%)",
        transformOrigin: "bottom center",
        willChange: "transform",
    },
    handle: {
        width: "2.5rem",
        height: "0.25rem",
        margin: "0 auto 0.75rem",
        borderRadius: "9999px",
        background: "var(--border)",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    backdrop: css({
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
        },
    }),
    drawer: css({
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
        },
    }),
};

export type DrawerPhase = "open" | "closing";

export function DrawerFrame({ children, label, onDismiss, phase }: PropsWithChildren<{ label: string; onDismiss: () => void; phase: DrawerPhase }>) {
    useEffect(() => {
        const previousOverflow = document.body.style.overflow;
        document.body.style.overflow = "hidden";
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== "Escape" || event.defaultPrevented) return;
            event.preventDefault();
            onDismiss();
        };
        document.addEventListener("keydown", onKeyDown);
        return () => {
            document.removeEventListener("keydown", onKeyDown);
            document.body.style.overflow = previousOverflow;
        };
    }, [onDismiss]);

    return createPortal(
        <div
            aria-hidden={phase === "closing" ? true : undefined}
            className={rules.backdrop}
            onPointerDown={(event) => {
                if (event.currentTarget === event.target) onDismiss();
            }}
            role="presentation"
            style={{
                ...styles.backdrop,
                animation: `${phase === "open" ? backdropOpen : backdropClose} ${phase === "open" ? 150 : 110}ms cubic-bezier(.2,.8,.2,1) both`,
                pointerEvents: phase === "closing" ? "none" : "auto",
            }}
        >
            <section
                aria-label={label}
                aria-modal="true"
                className={rules.drawer}
                data-drawer="bottom"
                role="dialog"
                style={{
                    ...styles.drawer,
                    animation: `${phase === "open" ? drawerOpen : drawerClose} ${phase === "open" ? 180 : 110}ms cubic-bezier(.2,.8,.2,1) both`,
                }}
            >
                <div aria-hidden="true" data-drawer-handle="" style={styles.handle} />
                {children}
            </section>
        </div>,
        document.body,
    );
}
