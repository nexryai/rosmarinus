import { type ComponentPropsWithRef, type CSSProperties, useEffect, useRef, useState } from "react";

import { keyframes } from "mintcss";

const rippleAnimation = keyframes({
    to: {
        boxShadow: "0 0 0 var(--ripple-radius) transparent",
    },
});

const styles = {
    layer: {
        position: "absolute",
        inset: 0,
        zIndex: 2,
        overflow: "hidden",
        borderRadius: "inherit",
        pointerEvents: "none",
    },
    ripple: {
        position: "absolute",
        width: 2,
        height: 2,
        borderRadius: "9999px",
        background: "transparent",
        boxShadow: "0 0 0 0 rgb(128 128 128 / 25%)",
        animation: `${rippleAnimation} 500ms cubic-bezier(0,.5,0,1) forwards`,
    },
} satisfies Record<string, CSSProperties>;

type Ripple = { id: number; x: number; y: number; radius: number };

export function RippleButton({ children, disabled, disableRipple = false, onKeyDown, onMouseDown, onPointerDown, style, type = "button", ...props }: ComponentPropsWithRef<"button"> & { disableRipple?: boolean }) {
    const [ripples, setRipples] = useState<Ripple[]>([]);
    const nextID = useRef(0);
    const timers = useRef(new Set<number>());
    const pointerStarted = useRef(false);

    useEffect(
        () => () => {
            for (const timer of timers.current) window.clearTimeout(timer);
        },
        [],
    );

    const addRipple = (button: HTMLButtonElement, clientX?: number, clientY?: number) => {
        if (disabled || disableRipple || document.documentElement.dataset.reduceMotion === "true" || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches) return;
        const rect = button.getBoundingClientRect();
        const x = clientX === undefined ? rect.width / 2 : clientX - rect.left;
        const y = clientY === undefined ? rect.height / 2 : clientY - rect.top;
        const radius = Math.max(Math.hypot(x, y), Math.hypot(rect.width - x, y), Math.hypot(x, rect.height - y), Math.hypot(rect.width - x, rect.height - y));
        const id = nextID.current++;
        setRipples((current) => [...current, { id, x, y, radius }]);
        const timer = window.setTimeout(() => {
            timers.current.delete(timer);
            setRipples((current) => current.filter((ripple) => ripple.id !== id));
        }, 500);
        timers.current.add(timer);
    };

    return (
        <button
            {...props}
            disabled={disabled}
            onKeyDown={(event) => {
                onKeyDown?.(event);
                if (!event.defaultPrevented && !event.repeat && (event.key === "Enter" || event.key === " ")) addRipple(event.currentTarget);
            }}
            onMouseDown={(event) => {
                onMouseDown?.(event);
                // Mouse compatibility events follow pointer events on touch and pen too.
                if (!pointerStarted.current && !event.defaultPrevented && event.button === 0) addRipple(event.currentTarget, event.clientX, event.clientY);
                pointerStarted.current = false;
            }}
            onPointerDown={(event) => {
                onPointerDown?.(event);
                pointerStarted.current = true;
                if (!event.defaultPrevented && event.button === 0 && event.isPrimary !== false) addRipple(event.currentTarget, event.clientX, event.clientY);
            }}
            style={{ position: "relative", ...style }}
            type={type}
        >
            {children}
            {ripples.length > 0 && (
                <span aria-hidden="true" style={styles.layer}>
                    {ripples.map((ripple) => (
                        <span data-testid="button-ripple" key={ripple.id} style={{ ...styles.ripple, left: ripple.x - 1, top: ripple.y - 1, "--ripple-radius": `${ripple.radius}px` } as CSSProperties} />
                    ))}
                </span>
            )}
        </button>
    );
}
