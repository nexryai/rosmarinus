import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Button } from "./ui";
import { Dropdown } from "./ui/Dropdown";

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

describe("Dropdown", () => {
    const options = [
        { value: "public", label: "公開" },
        { value: "home", label: "ホーム" },
        { value: "followers", label: "フォロワー", disabled: true },
    ];

    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        delete document.documentElement.dataset.reduceMotion;
    });

    it("opens a custom animated listbox and selects an option", () => {
        vi.useFakeTimers();
        const onChange = vi.fn();
        render(<Dropdown label="公開範囲" onChange={onChange} options={options} value="public" />);

        const trigger = screen.getByRole("button", { name: "公開範囲" });
        fireEvent.click(trigger);

        const listbox = screen.getByRole("listbox", { name: "公開範囲" });
        expect(trigger).toHaveAttribute("aria-expanded", "true");
        expect(listbox.style.animation).toContain("180ms");
        fireEvent.click(screen.getByRole("option", { name: "ホーム" }));

        expect(onChange).toHaveBeenCalledWith("home");
        expect(trigger).toHaveFocus();
        expect(screen.getByRole("listbox", { hidden: true }).style.animation).toContain("130ms");
        act(() => vi.advanceTimersByTime(130));
        expect(screen.queryByRole("listbox", { hidden: true })).not.toBeInTheDocument();
    });

    it("supports arrow keys, skips disabled options, and restores focus", () => {
        const onChange = vi.fn();
        render(<Dropdown label="公開範囲" onChange={onChange} options={options} value="public" />);
        const trigger = screen.getByRole("button", { name: "公開範囲" });

        fireEvent.keyDown(trigger, { key: "ArrowUp" });
        const listbox = screen.getByRole("listbox");
        expect(listbox).toHaveFocus();
        expect(listbox).toHaveAttribute("aria-activedescendant", expect.stringContaining("option-1"));
        fireEvent.keyDown(listbox, { key: "Enter" });

        expect(onChange).toHaveBeenCalledWith("home");
        expect(trigger).toHaveFocus();
    });

    it("closes immediately when reduced motion is enabled", () => {
        document.documentElement.dataset.reduceMotion = "true";
        render(<Dropdown label="テーマ" onChange={() => undefined} options={[{ value: "yellow", label: "Salvia Yellow" }]} value="yellow" />);

        fireEvent.click(screen.getByRole("button", { name: "テーマ" }));
        fireEvent.keyDown(screen.getByRole("listbox"), { key: "Escape" });

        expect(screen.queryByRole("listbox", { hidden: true })).not.toBeInTheDocument();
    });

    it("closes when the user interacts outside the dropdown", () => {
        vi.useFakeTimers();
        render(
            <div>
                <Dropdown label="テーマ" onChange={() => undefined} options={[{ value: "yellow", label: "Salvia Yellow" }]} value="yellow" />
                <button type="button">外側</button>
            </div>,
        );

        const trigger = screen.getByRole("button", { name: "テーマ" });
        fireEvent.click(trigger);
        fireEvent.pointerDown(screen.getByRole("button", { name: "外側" }));

        expect(trigger).toHaveAttribute("aria-expanded", "false");
        act(() => vi.advanceTimersByTime(130));
        expect(screen.queryByRole("listbox", { hidden: true })).not.toBeInTheDocument();
    });
});
