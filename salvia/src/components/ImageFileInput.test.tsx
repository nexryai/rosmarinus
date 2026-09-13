import { fireEvent, render } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ImageFileInput } from "./ImageFileInput";

describe("ImageFileInput", () => {
    it("accepts only the configured number of raster images and resets itself", () => {
        const onSelect = vi.fn();
        const { container } = render(<ImageFileInput maxFiles={1} multiple onSelect={onSelect} />);
        const input = container.querySelector("input") as HTMLInputElement;
        const first = new File(["one"], "one.png", { type: "image/png" });
        const second = new File(["two"], "two.webp", { type: "image/webp" });

        fireEvent.change(input, { target: { files: [first, second] } });

        expect(onSelect).toHaveBeenCalledWith([first]);
        expect(input.value).toBe("");
        expect(input.accept).toBe("image/jpeg,image/png,image/gif,image/webp");
    });
});
