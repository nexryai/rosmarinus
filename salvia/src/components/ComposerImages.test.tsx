import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { Composer } from "@/components/Composer";
import { api } from "@/lib/api";
import { preparePostImage } from "@/lib/postImage";
import type { Actor } from "@/lib/schema";
import { uploadImage } from "@/lib/uploader";

vi.mock("@/lib/postImage", () => ({ preparePostImage: vi.fn() }));
vi.mock("@/lib/uploader", () => ({ uploadImage: vi.fn() }));
const actor = { id: "alice", username: "alice", name: "Alice" } as Actor;
const original = new File(["original EXIF GPS"], "private.png", { type: "image/png" });
const encoded = new File(["encoded"], "image.webp", { type: "image/webp" });
function mount() {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    return { ...render(<Composer actor={actor} csrf="csrf" intent={{ kind: "post" }} onClose={vi.fn()} onSubmit={onSubmit} />), onSubmit };
}
describe("Composer image privacy", () => {
    beforeEach(() => {
        vi.spyOn(api, "emojis").mockResolvedValue([]);
        vi.mocked(preparePostImage).mockResolvedValue({ file: encoded, width: 4096, height: 2048, thumbnailURL: "blob:preview" });
        vi.mocked(uploadImage).mockResolvedValue({ id: "media-1", url: "https://proxy.test/image", preview_url: "https://proxy.test/image" });
        vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
    });
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
        vi.resetAllMocks();
    });
    it("uploads only encoded bytes with encoded dimensions, without asking about cropping", async () => {
        const { onSubmit } = mount();
        const user = userEvent.setup();
        await user.upload(screen.getByLabelText("画像"), original);
        await screen.findByAltText("image.webp");
        expect(screen.queryByRole("dialog", { name: "画像をクロップしますか？" })).not.toBeInTheDocument();
        await user.click(screen.getByRole("button", { name: "投稿する" }));
        expect(uploadImage).toHaveBeenCalledWith("csrf", "alice", encoded, { width: 4096, height: 2048 }, expect.any(String));
        expect(vi.mocked(uploadImage).mock.calls[0][2]).toBe(encoded);
        expect(vi.mocked(uploadImage).mock.calls[0][2]).not.toBe(original);
        expect(await vi.mocked(uploadImage).mock.calls[0][2].text()).toBe("encoded");
        expect(onSubmit).toHaveBeenCalledWith(expect.objectContaining({ media_ids: ["media-1"] }), expect.any(String));
    });
    it("blocks uploads and posting while image encoding is running", async () => {
        vi.mocked(preparePostImage).mockImplementation(() => new Promise(() => {}));
        mount();
        const user = userEvent.setup();
        await user.type(screen.getByLabelText("ノート本文"), "hello");
        await user.upload(screen.getByLabelText("画像"), original);
        expect(screen.getByRole("button", { name: "投稿する" })).toBeDisabled();
        expect(screen.getByLabelText("画像")).toBeDisabled();
        expect(uploadImage).not.toHaveBeenCalled();
    });
    it("does not upload originals when encoding fails", async () => {
        vi.mocked(preparePostImage).mockRejectedValueOnce(new Error("Encoding failed"));
        mount();
        await userEvent.setup().upload(screen.getByLabelText("画像"), original);
        await screen.findByText("Encoding failed");
        expect(uploadImage).not.toHaveBeenCalled();
        expect(screen.getByRole("button", { name: "投稿する" })).toBeDisabled();
    });
    it("aborts active workers and releases thumbnails when closed", async () => {
        const view = mount();
        await userEvent.setup().upload(screen.getByLabelText("画像"), original);
        await screen.findByAltText("image.webp");
        const signal = vi.mocked(preparePostImage).mock.calls[0][1];
        view.unmount();
        expect(signal.aborted).toBe(true);
        expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview");
    });
    it("releases earlier previews if a later image in the batch fails", async () => {
        vi.mocked(preparePostImage).mockResolvedValueOnce({ file: encoded, width: 100, height: 50, thumbnailURL: "blob:first" }).mockRejectedValueOnce(new Error("Second failed"));
        mount();
        fireEvent.change(screen.getByLabelText("画像"), { target: { files: [original, original] } });
        await waitFor(() => expect(screen.getByText("Second failed")).toBeInTheDocument());
        expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:first");
        expect(uploadImage).not.toHaveBeenCalled();
    });
});
