import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Button } from "./ui";

describe("Button ripple", () => {
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        delete document.documentElement.dataset.reduceMotion;
    });

    it("expands from the pointer position and disappears after the animation", () => {
        vi.useFakeTimers();
        render(<Button>投稿</Button>);
        const button = screen.getByRole("button", { name: "投稿" });
        vi.spyOn(button, "getBoundingClientRect").mockReturnValue({ bottom: 60, height: 40, left: 10, right: 110, top: 20, width: 100, x: 10, y: 20, toJSON: () => ({}) });

        fireEvent.mouseDown(button, { clientX: 35, clientY: 30 });

        const ripple = screen.getByTestId("button-ripple");
        expect(ripple).toHaveStyle({ left: "24px", top: "9px" });
        expect(ripple.style.getPropertyValue("--ripple-radius")).toBe(`${Math.hypot(75, 30)}px`);
        act(() => vi.advanceTimersByTime(500));
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
    });

    it("respects reduced motion and explicit opt-out", () => {
        const { rerender } = render(<Button disableRipple>戻る</Button>);
        fireEvent.mouseDown(screen.getByRole("button", { name: "戻る" }));
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();

        document.documentElement.dataset.reduceMotion = "true";
        rerender(<Button>戻る</Button>);
        fireEvent.mouseDown(screen.getByRole("button", { name: "戻る" }));
        expect(screen.queryByTestId("button-ripple")).not.toBeInTheDocument();
    });
});
