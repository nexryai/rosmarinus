import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Note } from "../lib/schema";
import { ImageViewer } from "./ImageViewer";

const images = [
    {
        media_type: "image/jpeg",
        name: "一枚目",
        sensitive: false,
        url: "https://media.example.test/one.jpg",
    },
    {
        media_type: "image/png",
        name: "二枚目",
        sensitive: false,
        url: "https://media.example.test/two.png",
    },
] as Note["attachments"];

describe("ImageViewer", () => {
    afterEach(() => {
        cleanup();
        vi.useRealTimers();
        delete document.documentElement.dataset.reduceMotion;
    });

    it("navigates a gallery with controls and the keyboard", () => {
        render(<ImageViewer images={images} initialIndex={0} onClose={vi.fn()} />);

        expect(screen.getByRole("dialog", { name: "画像ビューアー" })).toBeInTheDocument();
        expect(screen.getByAltText("一枚目")).toHaveAttribute("referrerpolicy", "no-referrer");
        expect(screen.getByText("1 / 2")).toBeInTheDocument();
        expect(screen.getByRole("link", { name: "元の画像を開く" })).toHaveAttribute("href", images[0].url);

        fireEvent.click(screen.getByRole("button", { name: "次の画像" }));
        expect(screen.getByAltText("二枚目")).toBeInTheDocument();
        expect(screen.getByText("2 / 2")).toBeInTheDocument();

        fireEvent.keyDown(document, { key: "ArrowLeft" });
        expect(screen.getByAltText("一枚目")).toBeInTheDocument();

        const dialog = screen.getByRole("dialog", { name: "画像ビューアー" });
        fireEvent.pointerDown(dialog, { clientX: 180, clientY: 100, pointerId: 1, pointerType: "touch" });
        fireEvent.pointerUp(dialog, { clientX: 80, clientY: 100, pointerId: 1, pointerType: "touch" });
        expect(screen.getByAltText("二枚目")).toBeInTheDocument();
    });

    it("zooms and closes with a fast exit animation", () => {
        vi.useFakeTimers();
        const onClose = vi.fn();
        render(<ImageViewer images={images} initialIndex={0} onClose={onClose} />);

        fireEvent.click(screen.getByRole("button", { name: "拡大" }));
        expect(screen.getByAltText("一枚目").style.transform).toContain("scale(1.5)");
        fireEvent.keyDown(document, { key: "Escape" });

        expect(onClose).not.toHaveBeenCalled();
        expect(screen.getByRole("dialog", { hidden: true }).style.animation).toContain("100ms");
        act(() => vi.advanceTimersByTime(100));
        expect(onClose).toHaveBeenCalledOnce();
    });

    it("closes immediately when reduced motion is enabled", () => {
        document.documentElement.dataset.reduceMotion = "true";
        const onClose = vi.fn();
        render(<ImageViewer images={images} initialIndex={0} onClose={onClose} />);

        fireEvent.click(screen.getByRole("button", { name: "閉じる" }));

        expect(onClose).toHaveBeenCalledOnce();
    });
});
