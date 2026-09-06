import { type CSSProperties, type KeyboardEvent, type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";

import { IconCheck, IconChevronDown } from "@tabler/icons-react";

import { css, keyframes } from "../../lib/css";

const openAnimation = keyframes({
    from: { opacity: 0, transform: "translateY(-0.4rem) scale(0.97)" },
    to: { opacity: 1, transform: "translateY(0) scale(1)" },
});

const closeAnimation = keyframes({
    from: { opacity: 1, transform: "translateY(0) scale(1)" },
    to: { opacity: 0, transform: "translateY(-0.3rem) scale(0.98)" },
});

const styles = {
    root: { position: "relative", minWidth: 0 },
    trigger: {
        width: "100%",
        minHeight: "2.75rem",
        padding: "0.625rem 0.75rem 0.625rem 0.875rem",
        display: "flex",
        alignItems: "center",
        gap: "0.625rem",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        color: "var(--text)",
        background: "var(--panel-muted)",
        textAlign: "left",
        transition: "border-color 160ms ease, background-color 160ms ease, box-shadow 160ms ease",
    },
    value: { minWidth: 0, flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
    chevron: { width: "1rem", height: "1rem", flexShrink: 0, color: "var(--muted)", transition: "transform 180ms cubic-bezier(.2,.8,.2,1)" },
    menu: {
        position: "absolute",
        zIndex: 80,
        right: 0,
        left: 0,
        minWidth: "12rem",
        maxHeight: "18rem",
        padding: "0.375rem",
        overflowY: "auto",
        border: "1px solid var(--border)",
        borderRadius: "1rem",
        background: "var(--panel)",
        boxShadow: "0 18px 45px rgb(42 36 24 / 18%), 0 3px 10px rgb(42 36 24 / 8%)",
        transformOrigin: "top center",
        willChange: "transform, opacity",
    },
    menuBottom: { top: "calc(100% + 0.5rem)" },
    menuTop: { bottom: "calc(100% + 0.5rem)", transformOrigin: "bottom center" },
    option: {
        width: "100%",
        minHeight: "2.5rem",
        padding: "0.5rem 0.625rem",
        display: "flex",
        alignItems: "center",
        gap: "0.625rem",
        borderRadius: "0.75rem",
        textAlign: "left",
        transition: "color 120ms ease, background-color 120ms ease, transform 120ms ease",
    },
    optionText: { minWidth: 0, flex: 1 },
    optionLabel: { display: "block", overflow: "hidden", fontSize: "0.875rem", fontWeight: 700, textOverflow: "ellipsis", whiteSpace: "nowrap" },
    description: { display: "block", marginTop: "0.0625rem", overflow: "hidden", color: "var(--muted)", fontSize: "0.7rem", textOverflow: "ellipsis", whiteSpace: "nowrap" },
    check: { width: "1rem", height: "1rem", flexShrink: 0, color: "var(--accent-hover)" },
} satisfies Record<string, CSSProperties>;

const rules = {
    trigger: css({
        "&:hover": { borderColor: "var(--accent-hover)", background: "var(--panel)" },
        '&[aria-expanded="true"]': { borderColor: "var(--accent-hover)", background: "var(--panel)", boxShadow: "0 0 0 3px var(--accent-soft)" },
        "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
    }),
    option: css({
        "&:hover": { background: "var(--panel-muted)" },
        '&[data-active="true"]': { background: "var(--accent-soft)", color: "var(--accent-ink)", transform: "translateX(0.125rem)" },
        "&:disabled": { opacity: 0.45 },
        "@media (prefers-reduced-motion: reduce)": { transitionDuration: "0.01ms" },
    }),
    menu: css({ "@media (prefers-reduced-motion: reduce)": { animationDuration: "0.01ms" } }),
};

export type DropdownOption<Value extends string> = {
    value: Value;
    label: ReactNode;
    description?: ReactNode;
    disabled?: boolean;
};

type DropdownProps<Value extends string> = {
    label: string;
    onChange: (value: Value) => void;
    options: DropdownOption<Value>[];
    value: Value;
    className?: string;
    placement?: "top" | "bottom";
    renderValue?: (option: DropdownOption<Value>) => ReactNode;
    style?: CSSProperties;
    triggerStyle?: CSSProperties;
};

const closeDuration = 130;
const motionIsReduced = () => document.documentElement.dataset.reduceMotion === "true" || window.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;

export function Dropdown<Value extends string>({ className = "", label, onChange, options, placement = "bottom", renderValue, style, triggerStyle, value }: DropdownProps<Value>) {
    const [phase, setPhase] = useState<"closed" | "open" | "closing">("closed");
    const [activeIndex, setActiveIndex] = useState(0);
    const rootRef = useRef<HTMLDivElement>(null);
    const triggerRef = useRef<HTMLButtonElement>(null);
    const menuRef = useRef<HTMLDivElement>(null);
    const closeTimer = useRef<number | undefined>(undefined);
    const listboxID = useId();
    const isOpen = phase === "open";
    const selectedIndex = Math.max(
        0,
        options.findIndex((option) => option.value === value),
    );
    const selected = options[selectedIndex];
    const hasEnabledOption = options.some((option) => !option.disabled);

    const clearCloseTimer = useCallback(() => {
        if (closeTimer.current !== undefined) window.clearTimeout(closeTimer.current);
        closeTimer.current = undefined;
    }, []);

    const close = useCallback(
        (restoreFocus = false) => {
            clearCloseTimer();
            if (phase === "closed") return;
            if (!restoreFocus) menuRef.current?.blur();
            if (motionIsReduced()) {
                setPhase("closed");
            } else {
                setPhase("closing");
                closeTimer.current = window.setTimeout(() => setPhase("closed"), closeDuration);
            }
            if (restoreFocus) triggerRef.current?.focus();
        },
        [clearCloseTimer, phase],
    );

    const open = (index = selectedIndex) => {
        clearCloseTimer();
        setActiveIndex(index);
        setPhase("open");
    };

    useEffect(() => {
        if (!isOpen) return;
        menuRef.current?.focus();
        const onPointerDown = (event: PointerEvent) => {
            if (!rootRef.current?.contains(event.target as Node)) close();
        };
        document.addEventListener("pointerdown", onPointerDown);
        return () => document.removeEventListener("pointerdown", onPointerDown);
    }, [close, isOpen]);

    useEffect(() => () => clearCloseTimer(), [clearCloseTimer]);

    const nextEnabled = (start: number, direction: 1 | -1) => {
        if (options.length === 0) return start;
        for (let offset = 1; offset <= options.length; offset += 1) {
            const index = (start + direction * offset + options.length) % options.length;
            if (!options[index]?.disabled) return index;
        }
        return start;
    };

    const choose = (index: number) => {
        const option = options[index];
        if (!option || option.disabled) return;
        if (option.value !== value) onChange(option.value);
        close(true);
    };

    const onTriggerKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
        if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
        event.preventDefault();
        open(nextEnabled(selectedIndex, event.key === "ArrowDown" ? 1 : -1));
    };

    const onMenuKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
        if (event.key === "Escape") {
            event.preventDefault();
            close(true);
            return;
        }
        if (event.key === "Tab") {
            close();
            return;
        }
        if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            choose(activeIndex);
            return;
        }
        if (event.key === "Home" || event.key === "End") {
            event.preventDefault();
            const start = event.key === "Home" ? -1 : options.length;
            setActiveIndex(nextEnabled(start, event.key === "Home" ? 1 : -1));
            return;
        }
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setActiveIndex((current) => nextEnabled(current, event.key === "ArrowDown" ? 1 : -1));
        }
    };

    return (
        <div className={className} ref={rootRef} style={{ ...styles.root, ...style }}>
            <button
                aria-controls={phase === "closed" ? undefined : listboxID}
                aria-expanded={isOpen}
                aria-haspopup="listbox"
                aria-label={label}
                className={rules.trigger}
                disabled={!hasEnabledOption}
                onClick={() => (isOpen ? close(true) : open())}
                onKeyDown={onTriggerKeyDown}
                ref={triggerRef}
                style={{ ...styles.trigger, ...triggerStyle }}
                type="button"
            >
                <span style={styles.value}>{selected && (renderValue ? renderValue(selected) : selected.label)}</span>
                <IconChevronDown aria-hidden="true" style={{ ...styles.chevron, transform: isOpen ? "rotate(180deg)" : "rotate(0)" }} />
            </button>
            {phase !== "closed" && (
                <div
                    aria-activedescendant={`${listboxID}-option-${activeIndex}`}
                    aria-hidden={phase === "closing" ? true : undefined}
                    aria-label={label}
                    className={rules.menu}
                    id={listboxID}
                    onKeyDown={onMenuKeyDown}
                    ref={menuRef}
                    role="listbox"
                    style={{
                        ...styles.menu,
                        ...(placement === "top" ? styles.menuTop : styles.menuBottom),
                        animation: `${phase === "open" ? openAnimation : closeAnimation} ${phase === "open" ? 180 : closeDuration}ms cubic-bezier(.2,.8,.2,1) both`,
                        pointerEvents: phase === "closing" ? "none" : "auto",
                    }}
                    tabIndex={-1}
                >
                    {options.map((option, index) => (
                        <button
                            aria-selected={option.value === value}
                            className={rules.option}
                            data-active={isOpen && index === activeIndex}
                            disabled={option.disabled}
                            id={`${listboxID}-option-${index}`}
                            key={option.value}
                            onClick={() => choose(index)}
                            onPointerMove={() => {
                                if (!option.disabled) setActiveIndex(index);
                            }}
                            role="option"
                            style={styles.option}
                            tabIndex={-1}
                            type="button"
                        >
                            <span style={styles.optionText}>
                                <span style={styles.optionLabel}>{option.label}</span>
                                {option.description && <span style={styles.description}>{option.description}</span>}
                            </span>
                            {option.value === value && <IconCheck aria-hidden="true" style={styles.check} />}
                        </button>
                    ))}
                </div>
            )}
        </div>
    );
}
