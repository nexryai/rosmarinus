import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Button, Modal } from "./ui";
import { ConfirmDialog } from "./ui/ConfirmDialog";
import { Dropdown } from "./ui/Dropdown";

const useMobileViewport = () => {
    vi.stubGlobal(
        "matchMedia",
        vi.fn((query: string) => ({
            matches: query === "(width < 64rem)",
            media: query,
            onchange: null,
            addEventListener: vi.fn(),
            removeEventListener: vi.fn(),
            addListener: vi.fn(),
            removeListener: vi.fn(),
            dispatchEvent: vi.fn(),
        })),
    );
};

describe("Button ripple", () => {
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        vi.unstubAllGlobals();
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

describe("Modal", () => {
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        vi.unstubAllGlobals();
        delete document.documentElement.dataset.reduceMotion;
    });

    it("animates quickly when it opens and closes", () => {
        vi.useFakeTimers();
        const onClose = vi.fn();
        render(
            <Modal label="投稿" onClose={onClose}>
                本文
            </Modal>,
        );

        const dialog = screen.getByRole("dialog", { name: "投稿" });
        expect(dialog.style.animation).toContain("160ms");
        expect(dialog.parentElement?.style.animation).toContain("140ms");

        fireEvent.click(screen.getByRole("button", { name: "閉じる" }));

        expect(onClose).not.toHaveBeenCalled();
        const closingDialog = screen.getByRole("dialog", { hidden: true });
        expect(closingDialog.style.animation).toContain("110ms");
        expect(closingDialog.parentElement).toHaveStyle({ pointerEvents: "none" });
        act(() => vi.advanceTimersByTime(110));
        expect(onClose).toHaveBeenCalledOnce();
    });

    it("closes immediately when reduced motion is enabled", () => {
        document.documentElement.dataset.reduceMotion = "true";
        const onClose = vi.fn();
        render(
            <Modal label="設定" onClose={onClose}>
                本文
            </Modal>,
        );

        fireEvent.keyDown(document, { key: "Escape" });

        expect(onClose).toHaveBeenCalledOnce();
    });

    it("uses a bottom drawer on mobile", () => {
        vi.useFakeTimers();
        useMobileViewport();
        const onClose = vi.fn();
        render(
            <Modal label="投稿" onClose={onClose}>
                本文
            </Modal>,
        );

        const drawer = screen.getByRole("dialog", { name: "投稿" });
        expect(drawer).toHaveAttribute("data-drawer", "bottom");
        expect(drawer.style.animation).toContain("180ms");
        expect(drawer.querySelector("[data-drawer-handle]")).toBeInTheDocument();
        expect(document.body).toHaveStyle({ overflow: "hidden" });

        fireEvent.pointerDown(drawer.parentElement as HTMLElement);
        act(() => vi.advanceTimersByTime(110));
        expect(onClose).toHaveBeenCalledOnce();
    });
});

describe("ConfirmDialog", () => {
    afterEach(() => {
        cleanup();
        vi.unstubAllGlobals();
    });

    it("lays out warning content on the left and actions on the right", () => {
        const onCancel = vi.fn();
        const onConfirm = vi.fn();
        render(
            <ConfirmDialog confirmLabel="削除する" onCancel={onCancel} onConfirm={onConfirm} title="ノートを削除しますか？">
                この操作は取り消せません。
            </ConfirmDialog>,
        );

        const dialog = screen.getByRole("dialog", { name: "ノートを削除しますか？" });
        expect(screen.getByRole("img", { name: "警告" })).toBeInTheDocument();
        expect(screen.getByRole("heading", { name: "ノートを削除しますか？" })).toHaveStyle({ textAlign: "left" });
        expect(screen.getByText("この操作は取り消せません。")).toHaveStyle({ textAlign: "left" });
        expect(dialog.querySelector('[data-confirm-dialog-part="actions"]')).toHaveStyle({ justifyContent: "flex-end" });

        fireEvent.click(screen.getByRole("button", { name: "キャンセル" }));
        fireEvent.click(screen.getByRole("button", { name: "削除する" }));
        expect(onCancel).toHaveBeenCalledOnce();
        expect(onConfirm).toHaveBeenCalledOnce();
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
        vi.unstubAllGlobals();
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

    it("shows mobile options in a bottom drawer", () => {
        vi.useFakeTimers();
        useMobileViewport();
        const onChange = vi.fn();
        render(<Dropdown label="公開範囲" onChange={onChange} options={options} value="public" />);

        fireEvent.click(screen.getByRole("button", { name: "公開範囲" }));

        const drawer = screen.getByRole("dialog", { name: "公開範囲の選択" });
        expect(drawer).toHaveAttribute("data-drawer", "bottom");
        expect(screen.getByRole("listbox", { name: "公開範囲" })).toHaveFocus();
        fireEvent.click(screen.getByRole("option", { name: "ホーム" }));

        expect(onChange).toHaveBeenCalledWith("home");
        act(() => vi.advanceTimersByTime(130));
        expect(screen.queryByRole("dialog", { hidden: true })).not.toBeInTheDocument();
    });
});
