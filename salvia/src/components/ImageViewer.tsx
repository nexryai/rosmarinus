import { type CSSProperties, type PointerEvent as ReactPointerEvent, useCallback, useEffect, useRef, useState, type WheelEvent } from "react";
import { createPortal } from "react-dom";

import { IconChevronLeft, IconChevronRight, IconExternalLink, IconMinus, IconPlus, IconX } from "@tabler/icons-react";

import { css, keyframes } from "../lib/css";
import type { Note } from "../lib/schema";

type ImageAttachment = Note["attachments"][number];
type Point = {
    x: number;
    y: number;
};
type Drag = Point & {
    pointerID: number;
    origin: Point;
};

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
const viewerOpen = keyframes({
    from: {
        opacity: 0,
        transform: "scale(0.97)",
    },
    to: {
        opacity: 1,
        transform: "scale(1)",
    },
});
const viewerClose = keyframes({
    from: {
        opacity: 1,
        transform: "scale(1)",
    },
    to: {
        opacity: 0,
        transform: "scale(0.985)",
    },
});
const imageFromRight = keyframes({
    from: {
        opacity: 0,
        transform: "translate3d(1.5rem, 0, 0)",
    },
    to: {
        opacity: 1,
        transform: "translate3d(0, 0, 0)",
    },
});
const imageFromLeft = keyframes({
    from: {
        opacity: 0,
        transform: "translate3d(-1.5rem, 0, 0)",
    },
    to: {
        opacity: 1,
        transform: "translate3d(0, 0, 0)",
    },
});

const styles = {
    overlay: {
        position: "fixed",
        zIndex: 200,
        inset: 0,
        display: "grid",
        gridTemplateRows: "auto minmax(0, 1fr) auto",
        overflow: "hidden",
        color: "#fff",
        background: "rgb(8 8 10 / 94%)",
        backdropFilter: "blur(12px)",
    },
    toolbar: {
        position: "relative",
        zIndex: 2,
        minHeight: "4rem",
        padding: "0.625rem max(0.75rem, env(safe-area-inset-right)) 0.625rem max(0.75rem, env(safe-area-inset-left))",
        display: "flex",
        alignItems: "center",
        gap: "0.5rem",
    },
    counter: {
        marginRight: "auto",
        paddingInline: "0.5rem",
        fontSize: "0.875rem",
        fontVariantNumeric: "tabular-nums",
    },
    iconButton: {
        width: "2.75rem",
        height: "2.75rem",
        display: "grid",
        placeItems: "center",
        borderRadius: "9999px",
        color: "#fff",
        background: "rgb(255 255 255 / 10%)",
        transition: "background-color 120ms ease, transform 120ms ease, opacity 120ms ease",
    },
    viewport: {
        position: "relative",
        minWidth: 0,
        minHeight: 0,
        display: "grid",
        placeItems: "center",
        overflow: "hidden",
        outline: "none",
        touchAction: "none",
        userSelect: "none",
    },
    image: {
        maxWidth: "calc(100vw - 2rem)",
        maxHeight: "calc(100dvh - 10rem)",
        objectFit: "contain",
        transformOrigin: "center",
        willChange: "transform",
    },
    error: {
        maxWidth: "24rem",
        padding: "1rem",
        color: "rgb(255 255 255 / 72%)",
        textAlign: "center",
    },
    navigation: {
        position: "absolute",
        zIndex: 2,
        top: "50%",
        transform: "translateY(-50%)",
    },
    previous: {
        left: "max(0.75rem, env(safe-area-inset-left))",
    },
    next: {
        right: "max(0.75rem, env(safe-area-inset-right))",
    },
    footer: {
        position: "relative",
        zIndex: 2,
        minHeight: "4rem",
        padding: "0.5rem max(1rem, env(safe-area-inset-right)) max(0.75rem, env(safe-area-inset-bottom)) max(1rem, env(safe-area-inset-left))",
        display: "grid",
        justifyItems: "center",
        gap: "0.5rem",
    },
    caption: {
        maxWidth: "min(36rem, 90vw)",
        overflow: "hidden",
        color: "rgb(255 255 255 / 78%)",
        fontSize: "0.8125rem",
        textOverflow: "ellipsis",
        whiteSpace: "nowrap",
    },
    thumbnails: {
        maxWidth: "min(38rem, 90vw)",
        display: "flex",
        gap: "0.375rem",
        overflowX: "auto",
    },
    thumbnailButton: {
        width: "3rem",
        height: "3rem",
        padding: "0.125rem",
        flexShrink: 0,
        overflow: "hidden",
        border: "2px solid transparent",
        borderRadius: "0.5rem",
        opacity: 0.55,
        transition: "border-color 120ms ease, opacity 120ms ease, transform 120ms ease",
    },
    thumbnailActive: {
        borderColor: "var(--accent)",
        opacity: 1,
    },
    thumbnail: {
        width: "100%",
        height: "100%",
        objectFit: "cover",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    overlay: css({
        "@media (prefers-reduced-motion: reduce)": {
            animationDuration: "0.01ms",
        },
    }),
    iconButton: css({
        "&:hover": {
            background: "rgb(255 255 255 / 20%)",
        },
        "&:active": {
            transform: "scale(0.94)",
        },
        "&:disabled": {
            opacity: 0.25,
        },
        "& > svg": {
            width: "1.25rem",
            height: "1.25rem",
        },
    }),
    thumbnailButton: css({
        "&:hover": {
            opacity: 1,
            transform: "translateY(-0.125rem)",
        },
    }),
};

const closeDuration = 100;
const clampScale = (value: number) => Math.min(4, Math.max(1, value));
const motionIsReduced = () => document.documentElement.dataset.reduceMotion === "true" || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;

export function ImageViewer({ images, initialIndex, onClose }: { images: ImageAttachment[]; initialIndex: number; onClose: () => void }) {
    const [phase, setPhase] = useState<"open" | "closing">("open");
    const [index, setIndex] = useState(() => Math.min(Math.max(initialIndex, 0), images.length - 1));
    const [direction, setDirection] = useState<"left" | "right">("right");
    const [scale, setScale] = useState(1);
    const [offset, setOffset] = useState<Point>({
        x: 0,
        y: 0,
    });
    const [failed, setFailed] = useState(false);
    const closeTimer = useRef<number | undefined>(undefined);
    const drag = useRef<Drag | undefined>(undefined);
    const previousFocus = useRef<HTMLElement | null>(null);
    const viewportRef = useRef<HTMLDivElement>(null);
    const image = images[index];

    const resetTransform = useCallback(() => {
        setScale(1);
        setOffset({
            x: 0,
            y: 0,
        });
        setFailed(false);
    }, []);

    const goTo = useCallback(
        (nextIndex: number) => {
            const boundedIndex = Math.min(Math.max(nextIndex, 0), images.length - 1);
            if (boundedIndex === index) return;
            setDirection(boundedIndex > index ? "right" : "left");
            setIndex(boundedIndex);
            resetTransform();
        },
        [images.length, index, resetTransform],
    );

    const requestClose = useCallback(() => {
        if (phase === "closing") return;
        if (motionIsReduced()) {
            onClose();
            return;
        }
        setPhase("closing");
        closeTimer.current = window.setTimeout(onClose, closeDuration);
    }, [onClose, phase]);

    const zoom = useCallback((amount: number) => {
        setScale((current) => {
            const next = clampScale(current + amount);
            if (next === 1)
                setOffset({
                    x: 0,
                    y: 0,
                });
            return next;
        });
    }, []);

    useEffect(() => {
        const previousOverflow = document.body.style.overflow;
        previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        document.body.style.overflow = "hidden";
        viewportRef.current?.focus();
        return () => {
            document.body.style.overflow = previousOverflow;
            previousFocus.current?.focus();
            if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
        };
    }, []);

    useEffect(() => {
        const onKeyDown = (event: globalThis.KeyboardEvent) => {
            if (event.key === "Escape") requestClose();
            if (event.key === "ArrowLeft") goTo(index - 1);
            if (event.key === "ArrowRight") goTo(index + 1);
            if (event.key === "+" || event.key === "=") zoom(0.5);
            if (event.key === "-") zoom(-0.5);
            if (["Escape", "ArrowLeft", "ArrowRight", "+", "=", "-"].includes(event.key)) event.preventDefault();
        };
        document.addEventListener("keydown", onKeyDown);
        return () => document.removeEventListener("keydown", onKeyDown);
    }, [goTo, index, requestClose, zoom]);

    const onPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
        if (event.pointerType === "mouse" && event.button !== 0) return;
        drag.current = {
            pointerID: event.pointerId,
            x: event.clientX,
            y: event.clientY,
            origin: offset,
        };
        event.currentTarget.setPointerCapture?.(event.pointerId);
    };

    const onPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
        if (!drag.current || drag.current.pointerID !== event.pointerId) return;
        const deltaX = event.clientX - drag.current.x;
        const deltaY = event.clientY - drag.current.y;
        setOffset({
            x: drag.current.origin.x + deltaX,
            y: scale > 1 ? drag.current.origin.y + deltaY : 0,
        });
    };

    const onPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
        if (!drag.current || drag.current.pointerID !== event.pointerId) return;
        const deltaX = event.clientX - drag.current.x;
        drag.current = undefined;
        event.currentTarget.releasePointerCapture?.(event.pointerId);
        if (scale === 1 && Math.abs(deltaX) >= 64) goTo(index + (deltaX < 0 ? 1 : -1));
        if (scale === 1)
            setOffset({
                x: 0,
                y: 0,
            });
    };

    const onWheel = (event: WheelEvent<HTMLDivElement>) => {
        event.preventDefault();
        zoom(event.deltaY < 0 ? 0.35 : -0.35);
    };

    if (!image) return null;

    return createPortal(
        <div
            aria-hidden={phase === "closing" ? true : undefined}
            className={rules.overlay}
            onMouseDown={(event) => {
                if (event.currentTarget === event.target) requestClose();
            }}
            role="presentation"
            style={{
                ...styles.overlay,
                animation: `${phase === "open" ? backdropOpen : backdropClose} ${phase === "open" ? 120 : closeDuration}ms ease-out both`,
                pointerEvents: phase === "closing" ? "none" : "auto",
            }}
        >
            <header style={styles.toolbar}>
                <span aria-live="polite" style={styles.counter}>
                    {index + 1} / {images.length}
                </span>
                <button aria-label="縮小" className={rules.iconButton} disabled={scale === 1} onClick={() => zoom(-0.5)} style={styles.iconButton} type="button">
                    <IconMinus />
                </button>
                <button aria-label="拡大" className={rules.iconButton} disabled={scale === 4} onClick={() => zoom(0.5)} style={styles.iconButton} type="button">
                    <IconPlus />
                </button>
                <a aria-label="元の画像を開く" className={rules.iconButton} href={image.url} rel="noreferrer" style={styles.iconButton} target="_blank">
                    <IconExternalLink />
                </a>
                <button aria-label="閉じる" className={rules.iconButton} onClick={requestClose} style={styles.iconButton} type="button">
                    <IconX />
                </button>
            </header>
            <div
                aria-label="画像ビューアー"
                aria-modal="true"
                onClick={(event) => {
                    if (event.currentTarget === event.target) requestClose();
                }}
                onDoubleClick={() => {
                    setScale((current) => (current === 1 ? 2 : 1));
                    setOffset({
                        x: 0,
                        y: 0,
                    });
                }}
                onPointerCancel={onPointerUp}
                onPointerDown={onPointerDown}
                onPointerMove={onPointerMove}
                onPointerUp={onPointerUp}
                onWheel={onWheel}
                ref={viewportRef}
                role="dialog"
                style={{
                    ...styles.viewport,
                    animation: `${phase === "open" ? viewerOpen : viewerClose} ${phase === "open" ? 150 : closeDuration}ms cubic-bezier(.2,.8,.2,1) both`,
                    cursor: scale > 1 ? "grab" : "zoom-in",
                }}
                tabIndex={-1}
            >
                {failed ? (
                    <p role="alert" style={styles.error}>
                        画像を読み込めませんでした。
                    </p>
                ) : (
                    <img
                        alt={image.name || "添付画像"}
                        draggable={false}
                        key={`${index}:${image.url}`}
                        onError={() => setFailed(true)}
                        referrerPolicy="no-referrer"
                        src={image.url}
                        style={{
                            ...styles.image,
                            animation: scale === 1 ? `${direction === "right" ? imageFromRight : imageFromLeft} 160ms cubic-bezier(.2,.8,.2,1)` : undefined,
                            transform: `translate3d(${offset.x}px, ${offset.y}px, 0) scale(${scale})`,
                            transition: drag.current ? "none" : "transform 140ms cubic-bezier(.2,.8,.2,1)",
                        }}
                    />
                )}
                {images.length > 1 && (
                    <>
                        <button aria-label="前の画像" className={`${rules.iconButton}`} disabled={index === 0} onClick={() => goTo(index - 1)} style={{ ...styles.iconButton, ...styles.navigation, ...styles.previous }} type="button">
                            <IconChevronLeft />
                        </button>
                        <button aria-label="次の画像" className={`${rules.iconButton}`} disabled={index === images.length - 1} onClick={() => goTo(index + 1)} style={{ ...styles.iconButton, ...styles.navigation, ...styles.next }} type="button">
                            <IconChevronRight />
                        </button>
                    </>
                )}
            </div>
            <footer style={styles.footer}>
                <p style={styles.caption}>{image.name || "添付画像"}</p>
                {images.length > 1 && (
                    <div aria-label="画像一覧" role="group" style={styles.thumbnails}>
                        {images.map((thumbnail, thumbnailIndex) => (
                            <button
                                aria-label={`${thumbnailIndex + 1}枚目の画像を表示`}
                                aria-pressed={thumbnailIndex === index}
                                className={rules.thumbnailButton}
                                key={thumbnail.url}
                                onClick={() => goTo(thumbnailIndex)}
                                style={{
                                    ...styles.thumbnailButton,
                                    ...(thumbnailIndex === index ? styles.thumbnailActive : {}),
                                }}
                                type="button"
                            >
                                <img alt="" loading="lazy" referrerPolicy="no-referrer" src={thumbnail.url} style={styles.thumbnail} />
                            </button>
                        ))}
                    </div>
                )}
            </footer>
        </div>,
        document.body,
    );
}
