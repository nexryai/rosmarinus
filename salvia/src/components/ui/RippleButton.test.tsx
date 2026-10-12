import { createRef } from "react";

import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RoundButton } from "@/components/ui";
import { Avatar } from "@/components/ui/Avatar";
import { RippleButton } from "@/components/ui/RippleButton";

afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.unstubAllGlobals();
    delete document.documentElement.dataset.reduceMotion;
});

const measure = (button: HTMLElement) => vi.spyOn(button, "getBoundingClientRect").mockReturnValue({ bottom: 60, height: 40, left: 10, right: 110, top: 20, width: 100, x: 10, y: 20, toJSON: () => ({}) });

const usePointerEvents = () => {
    vi.stubGlobal(
        "PointerEvent",
        class extends MouseEvent {
            pointerType: string;
            isPrimary: boolean;
            constructor(type: string, options: PointerEventInit = {}) {
                super(type, options);
                this.pointerType = options.pointerType ?? "mouse";
                this.isPrimary = options.isPrimary ?? true;
            }
        },
    );
};

describe("RippleButton", () => {
    it.each(["mouse", "touch", "pen"])("uses the %s pointer position without duplicating compatibility mouse events", (pointerType) => {
        usePointerEvents();
        const onPointerDown = vi.fn();
        const onMouseDown = vi.fn();
        render(
            <RippleButton onMouseDown={onMouseDown} onPointerDown={onPointerDown}>
                操作
            </RippleButton>,
        );
        const button = screen.getByRole("button", { name: "操作" });
        measure(button);
        fireEvent.pointerDown(button, { pointerType, clientX: 35, clientY: 30, button: 0 });
        fireEvent.mouseDown(button, { clientX: 35, clientY: 30 });
        expect(screen.getAllByTestId("button-ripple")).toHaveLength(1);
        expect(screen.getByTestId("button-ripple")).toHaveStyle({ left: "24px", top: "9px" });
        expect(onPointerDown).toHaveBeenCalledOnce();
        expect(onMouseDown).toHaveBeenCalledOnce();
    });

    it.each(["Enter", " "])("centers keyboard feedback for %s without changing activation behavior", (key) => {
        const onClick = vi.fn();
        render(<RippleButton onClick={onClick}>操作</RippleButton>);
        const button = screen.getByRole("button", { name: "操作" });
        measure(button);
        fireEvent.keyDown(button, { key });
        expect(screen.getByTestId("button-ripple")).toHaveStyle({ left: "49px", top: "19px" });
        fireEvent.keyDown(button, { key, repeat: true });
        expect(screen.getAllByTestId("button-ripple")).toHaveLength(1);
        fireEvent.click(button);
        expect(onClick).toHaveBeenCalledOnce();
    });

    it("does not animate disabled buttons, canceled events, or right clicks", () => {
        const { rerender } = render(<RippleButton disabled>操作</RippleButton>);
        fireEvent.mouseDown(screen.getByRole("button"));
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
        rerender(
            <RippleButton onMouseDown={(event) => event.preventDefault()} onKeyDown={(event) => event.preventDefault()}>
                操作
            </RippleButton>,
        );
        fireEvent.mouseDown(screen.getByRole("button"));
        fireEvent.keyDown(screen.getByRole("button"), { key: "Enter" });
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
        rerender(<RippleButton>操作</RippleButton>);
        fireEvent.mouseDown(screen.getByRole("button"), { button: 2 });
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
    });

    it("respects OS reduced motion", () => {
        vi.stubGlobal(
            "matchMedia",
            vi.fn(() => ({ matches: true })),
        );
        render(<RippleButton>操作</RippleButton>);
        fireEvent.mouseDown(screen.getByRole("button"));
        fireEvent.keyDown(screen.getByRole("button"), { key: "Enter" });
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
    });

    it("preserves refs, positioning, children, and releases pending timers on unmount", () => {
        vi.useFakeTimers();
        const ref = createRef<HTMLButtonElement>();
        const { unmount } = render(
            <RippleButton ref={ref} style={{ position: "absolute", top: "1rem" }}>
                <span>操作</span>
            </RippleButton>,
        );
        expect(ref.current).toBe(screen.getByRole("button"));
        expect(ref.current?.style.position).toBe("absolute");
        expect(ref.current?.style.top).toBe("1rem");
        fireEvent.mouseDown(screen.getByRole("button"));
        expect(vi.getTimerCount()).toBe(1);
        expect(screen.getByTestId("button-ripple").parentElement).toHaveStyle({ pointerEvents: "none", overflow: "hidden" });
        act(() => vi.advanceTimersByTime(700));
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
        fireEvent.mouseDown(screen.getByRole("button"));
        unmount();
        expect(vi.getTimerCount()).toBe(0);
    });

    it("provides the same feedback for round buttons and profile avatars", () => {
        render(
            <>
                <RoundButton aria-label="戻る" />
                <Avatar actor={{ id: "actor", name: "Alice", username: "alice", avatar_url: "" }} onOpenProfile={vi.fn()} />
            </>,
        );
        fireEvent.mouseDown(screen.getByRole("button", { name: "戻る" }));
        fireEvent.mouseDown(screen.getByRole("button", { name: "Aliceのプロフィールを開く" }));
        expect(screen.getAllByTestId("button-ripple")).toHaveLength(2);
    });
});
