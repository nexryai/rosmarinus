import { type CSSProperties, type KeyboardEvent, type ReactElement, type ReactNode, useLayoutEffect, useRef, useState } from "react";

import { css } from "../../lib/css";

type TabOption<T extends string> = {
    value: T;
    label: ReactNode;
    disabled?: boolean;
};

type UnderlineTabOption<T extends string> = TabOption<T> & { icon: ReactElement };

type TabsProps<T extends string> = {
    ariaLabel: string;
    onChange: (value: T) => void;
    value: T;
    className?: string;
    style?: CSSProperties;
} & ({ variant: "underline"; items: readonly UnderlineTabOption<T>[] } | { variant?: "pill"; items: readonly TabOption<T>[] });

const rules = {
    root: css({
        position: "relative",
        display: "flex",
        alignItems: "stretch",
        gap: "0.5rem",
        borderBottom: "1px solid var(--border)",
    }),
    pillRoot: css({
        paddingBlock: "0.75rem",
    }),
    tab: css({
        position: "relative",
        zIndex: 1,
        minWidth: 0,
        borderRadius: "9999px",
        color: "var(--muted)",
        fontWeight: 700,
        transition: "color 180ms cubic-bezier(.4, 0, .2, 1)",
        "&:focus-visible": {
            outline: "2px solid var(--accent-hover)",
            outlineOffset: "2px",
        },
        "@media (prefers-reduced-motion: reduce)": {
            transitionDuration: "0.01ms",
        },
        'html[data-reduce-motion="true"] &': {
            transitionDuration: "0.01ms",
        },
    }),
    pillTab: css({
        flex: "1 1 0",
        padding: "0.625rem 0.75rem",
        fontSize: "0.8125rem",
        textAlign: "center",
    }),
    underlineTab: css({
        flex: "0 0 auto",
        padding: "0.75rem 1rem",
        fontWeight: 800,
    }),
    tabContent: css({
        display: "inline-flex",
        alignItems: "center",
        justifyContent: "center",
        gap: "0.375rem",
        whiteSpace: "nowrap",
        "& svg": {
            width: "1.125rem",
            height: "1.125rem",
            flexShrink: 0,
        },
    }),
    pillSelected: css({
        color: "var(--accent-ink)",
    }),
    underlineSelected: css({
        color: "var(--text)",
    }),
    indicator: css({
        position: "absolute",
        zIndex: 0,
        pointerEvents: "none",
        transition: "left 220ms cubic-bezier(.2, .8, .2, 1), top 220ms cubic-bezier(.2, .8, .2, 1), width 220ms cubic-bezier(.2, .8, .2, 1), height 220ms cubic-bezier(.2, .8, .2, 1), opacity 120ms ease",
        "@media (prefers-reduced-motion: reduce)": {
            transitionDuration: "0.01ms",
        },
        'html[data-reduce-motion="true"] &': {
            transitionDuration: "0.01ms",
        },
    }),
    pillIndicator: css({
        borderRadius: "9999px",
        background: "var(--accent-soft)",
    }),
    underlineIndicator: css({
        borderRadius: "9999px",
        background: "var(--accent-hover)",
    }),
};

export function Tabs<T extends string>({ ariaLabel, className = "", items, onChange, value, variant = "pill", style }: TabsProps<T>) {
    const rootRef = useRef<HTMLDivElement>(null);
    const tabRefs = useRef(new Map<T, HTMLButtonElement>());
    const [indicator, setIndicator] = useState<CSSProperties>({ opacity: 0 });
    useLayoutEffect(() => {
        const root = rootRef.current;
        const selectedTab = tabRefs.current.get(value);
        if (!root || !selectedTab || !items.some((item) => item.value === value)) return;

        const measure = () => {
            const rootRect = root.getBoundingClientRect();
            const tabRect = selectedTab.getBoundingClientRect();
            setIndicator({
                left: tabRect.left - rootRect.left,
                top: variant === "pill" ? tabRect.top - rootRect.top : undefined,
                bottom: variant === "underline" ? -1 : undefined,
                width: tabRect.width,
                height: variant === "pill" ? tabRect.height : 2,
                opacity: 1,
            });
        };

        measure();
        const observer = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(measure);
        observer?.observe(root);
        for (const tab of tabRefs.current.values()) observer?.observe(tab);
        window.addEventListener("resize", measure);
        return () => {
            observer?.disconnect();
            window.removeEventListener("resize", measure);
        };
    }, [items, value, variant]);

    const handleKeyDown = (event: KeyboardEvent<HTMLButtonElement>, index: number) => {
        const enabledItems = items.filter((item) => !item.disabled);
        if (enabledItems.length < 2) return;

        const currentIndex = enabledItems.findIndex((item) => item.value === items[index]?.value);
        const nextIndex = event.key === "ArrowRight" ? (currentIndex + 1) % enabledItems.length : event.key === "ArrowLeft" ? (currentIndex - 1 + enabledItems.length) % enabledItems.length : event.key === "Home" ? 0 : event.key === "End" ? enabledItems.length - 1 : -1;
        if (nextIndex < 0) return;

        event.preventDefault();
        const next = enabledItems[nextIndex];
        onChange(next.value);
        tabRefs.current.get(next.value)?.focus();
    };

    return (
        <div aria-label={ariaLabel} aria-orientation="horizontal" className={`${rules.root} ${variant === "pill" ? rules.pillRoot : ""} ${className}`} ref={rootRef} role="tablist" style={style}>
            <span aria-hidden="true" className={`${rules.indicator} ${variant === "pill" ? rules.pillIndicator : rules.underlineIndicator}`} style={indicator} />
            {items.map((item, index) => {
                const selected = item.value === value;
                return (
                    <button
                        aria-selected={selected}
                        className={`${rules.tab} ${variant === "pill" ? rules.pillTab : rules.underlineTab} ${selected ? (variant === "pill" ? rules.pillSelected : rules.underlineSelected) : ""}`}
                        disabled={item.disabled}
                        key={item.value}
                        onClick={() => onChange(item.value)}
                        onKeyDown={(event) => handleKeyDown(event, index)}
                        ref={(node) => {
                            if (node) tabRefs.current.set(item.value, node);
                            else tabRefs.current.delete(item.value);
                        }}
                        role="tab"
                        tabIndex={selected ? 0 : -1}
                        type="button"
                    >
                        <span className={rules.tabContent}>
                            {"icon" in item && <span aria-hidden="true">{item.icon}</span>}
                            {item.label}
                        </span>
                    </button>
                );
            })}
        </div>
    );
}
