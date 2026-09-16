import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { Note } from "../lib/schema";
import { NoteMedia } from "./NoteMedia";

const image = (index: number) =>
    ({
        media_type: "image/jpeg",
        name: `画像${index}`,
        sensitive: false,
        url: `https://media.example.test/${index}.jpg`,
        thumbnail_url: `https://media.example.test/${index}-thumbnail.jpg`,
    }) as Note["attachments"][number];

describe("NoteMedia", () => {
    afterEach(cleanup);

    it("shows one image without cropping and caps its expanded height like Misskey", () => {
        render(<NoteMedia attachments={[image(1)]} onOpenImage={vi.fn()} />);

        const gallery = document.querySelector<HTMLElement>('[data-image-count="1"]');
        const renderedImage = screen.getByAltText("画像1");
        expect(gallery?.style.maxHeight).toBe("min(22.5rem, 50vh)");
        expect(renderedImage.style.objectFit).toBe("contain");
        expect(renderedImage.style.height).toBe("auto");
        expect(renderedImage).toHaveAttribute("src", image(1).thumbnail_url);
    });

    it("uses Misskey's 16:9 arrangements for two, three, and four images", () => {
        const { rerender } = render(<NoteMedia attachments={[image(1), image(2)]} onOpenImage={vi.fn()} />);
        let gallery = document.querySelector<HTMLElement>('[data-image-count="2"]');
        expect(gallery?.style.aspectRatio).toBe("16 / 9");
        expect(gallery?.style.gridTemplateColumns).toBe("1fr 1fr");

        rerender(<NoteMedia attachments={[image(1), image(2), image(3)]} onOpenImage={vi.fn()} />);
        gallery = document.querySelector<HTMLElement>('[data-image-count="3"]');
        expect(gallery?.style.gridTemplateColumns).toBe("1fr 0.5fr");
        expect(screen.getByRole("button", { name: "画像を表示: 画像1" }).style.gridRow).toBe("1 / 3");

        rerender(<NoteMedia attachments={[image(1), image(2), image(3), image(4)]} onOpenImage={vi.fn()} />);
        gallery = document.querySelector<HTMLElement>('[data-image-count="4"]');
        expect(gallery?.style.gridTemplateRows).toBe("1fr 1fr");
    });

    it("keeps image indexes independent from non-image attachments", () => {
        const onOpenImage = vi.fn();
        const file = { media_type: "application/pdf", name: "資料", sensitive: false, url: "https://media.example.test/file.pdf" } as Note["attachments"][number];
        render(<NoteMedia attachments={[file, image(1), image(2)]} onOpenImage={onOpenImage} />);

        fireEvent.click(screen.getByRole("button", { name: "画像を表示: 画像2" }));
        expect(onOpenImage).toHaveBeenCalledWith(1);
        expect(screen.getByRole("link", { name: "資料" })).toHaveAttribute("href", file.url);
    });
});
