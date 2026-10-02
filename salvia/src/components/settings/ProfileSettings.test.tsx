import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ProfileSettings } from "@/components/settings/ProfileSettings";
import { api } from "@/lib/api";
import { processProfileImage } from "@/lib/profileImage";
import type { Actor } from "@/lib/schema";
import { uploadImage } from "@/lib/uploader";

vi.mock("@/lib/profileImage", () => ({ processProfileImage: vi.fn() }));
vi.mock("@/lib/uploader", () => ({ uploadImage: vi.fn() }));
const actor = { id: "alice", username: "alice", name: "Alice", summary: "", avatar_url: "", banner_url: "", emojis: [] } as unknown as Actor;
const original = new File(["original"], "image.gif", { type: "image/gif" });
function mount() {
    return render(<ProfileSettings actor={actor} csrf="csrf" onActorsChanged={vi.fn().mockResolvedValue(undefined)} />);
}
async function select(label = "アバター画像") {
    await userEvent.setup().upload(screen.getByLabelText(label), original);
    await screen.findByRole("dialog", { name: "画像をクロップしますか？" });
}

describe("profile editor", () => {
    beforeEach(() => {
        vi.mocked(processProfileImage).mockResolvedValue({ blob: new Blob(["preview"], { type: "image/webp" }), width: 1200, height: 800 });
        vi.mocked(uploadImage).mockResolvedValue({ id: "media", url: "https://proxy.example/image", preview_url: "https://proxy.example/image", source_url: "https://objects.example/image" });
        vi.spyOn(api, "updateActor").mockResolvedValue(undefined);
        vi.stubGlobal(
            "URL",
            class extends URL {
                static createObjectURL = vi.fn(() => "blob:preview");
                static revokeObjectURL = vi.fn();
            },
        );
    });
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
        vi.clearAllMocks();
        vi.unstubAllGlobals();
    });
    it("previews name and MFM summary before saving", async () => {
        mount();
        const user = userEvent.setup();
        await user.clear(screen.getByLabelText("表示名"));
        await user.type(screen.getByLabelText("表示名"), "New name");
        await user.type(screen.getByLabelText("自己紹介"), "**Hello**");
        const preview = within(screen.getByRole("region", { name: "プロフィールプレビュー" }));
        expect(preview.getByRole("heading", { name: "New name" })).toBeInTheDocument();
        expect(preview.getByText("Hello")).toBeInTheDocument();
        expect(api.updateActor).not.toHaveBeenCalled();
    });
    it("uploads the original bytes only on save when cropping is declined", async () => {
        mount();
        await select();
        fireEvent.click(screen.getByRole("button", { name: "いいえ、そのまま使う" }));
        expect(uploadImage).not.toHaveBeenCalled();
        fireEvent.click(screen.getByRole("button", { name: "プロフィールを保存" }));
        await waitFor(() => expect(uploadImage).toHaveBeenCalledWith("csrf", "alice", original, expect.objectContaining({ width: 1200, height: 800 })));
        await waitFor(() => expect(api.updateActor).toHaveBeenCalledWith("csrf", "alice", { name: "Alice", summary: "", avatar_url: "https://objects.example/image" }));
    });
    it("crops a banner at 3:1 in the worker and uploads WebP with output dimensions", async () => {
        mount();
        await select("バナー画像");
        fireEvent.click(screen.getByRole("button", { name: "はい、クロップする" }));
        fireEvent.change(screen.getByLabelText("横位置"), { target: { value: "0.8" } });
        vi.mocked(processProfileImage).mockResolvedValueOnce({ blob: new Blob(["cropped"], { type: "image/webp" }), width: 900, height: 300 });
        fireEvent.click(screen.getByRole("button", { name: "クロップを適用" }));
        await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
        expect(processProfileImage).toHaveBeenLastCalledWith(original, expect.any(AbortSignal), { aspect: 3, zoom: 1, x: 0.8, y: 0.5 });
        fireEvent.click(screen.getByRole("button", { name: "プロフィールを保存" }));
        await waitFor(() => expect(uploadImage).toHaveBeenCalled());
        const file = vi.mocked(uploadImage).mock.calls[0][2];
        expect(file.name).toBe("banner.webp");
        expect(file.type).toBe("image/webp");
        expect(vi.mocked(uploadImage).mock.calls[0][3]).toMatchObject({ width: 900, height: 300 });
        await waitFor(() => expect(api.updateActor).toHaveBeenCalledWith("csrf", "alice", expect.objectContaining({ banner_url: "https://objects.example/image" })));
    });
    it("cancels without uploading and releases the preview", async () => {
        mount();
        await select();
        fireEvent.click(screen.getByRole("button", { name: "キャンセル" }));
        expect(uploadImage).not.toHaveBeenCalled();
        expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview");
    });
    it("retains a completed upload when a profile mutation fails", async () => {
        vi.mocked(api.updateActor).mockRejectedValueOnce(new Error("Try again"));
        mount();
        await select();
        fireEvent.click(screen.getByRole("button", { name: "いいえ、そのまま使う" }));
        fireEvent.click(screen.getByRole("button", { name: "プロフィールを保存" }));
        await screen.findByText("Try again");
        fireEvent.click(screen.getByRole("button", { name: "プロフィールを保存" }));
        await screen.findByText("プロフィールを保存しました");
        expect(uploadImage).toHaveBeenCalledOnce();
        expect(api.updateActor).toHaveBeenCalledTimes(2);
    });
    it("aborts image processing on unmount", async () => {
        vi.mocked(processProfileImage).mockImplementation(() => new Promise(() => {}));
        const view = mount();
        await userEvent.setup().upload(screen.getByLabelText("アバター画像"), original);
        const signal = vi.mocked(processProfileImage).mock.calls[0][1];
        view.unmount();
        expect(signal.aborted).toBe(true);
    });
});
