import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SecuritySettings } from "@/components/settings/SecuritySettings";
import { api } from "@/lib/api";
import { createPasskey } from "@/lib/webauthn";

vi.mock("@/lib/webauthn", () => ({ createPasskey: vi.fn() }));

const phone = { id: "phone-key", name: "Phone", created_at: "2026-09-01T00:00:00Z" };
const laptop = { id: "laptop-key", name: "Laptop", created_at: "2026-10-01T00:00:00Z" };

describe("SecuritySettings", () => {
    afterEach(() => {
        cleanup();
        vi.restoreAllMocks();
    });

    it("registers, renames, and deletes passkeys while protecting the last one", async () => {
        vi.spyOn(api, "passkeys").mockResolvedValue([phone]);
        const start = vi.spyOn(api, "passkeyRegistrationStart").mockResolvedValue({ ceremony_id: "ceremony", public_key: { challenge: "challenge" } });
        vi.mocked(createPasskey).mockResolvedValue({ id: "new-credential" });
        const finish = vi.spyOn(api, "passkeyRegistrationFinish").mockResolvedValue([phone, laptop]);
        const rename = vi.spyOn(api, "renamePasskey").mockResolvedValue(undefined);
        const remove = vi.spyOn(api, "deletePasskey").mockResolvedValue(undefined);
        const user = userEvent.setup();
        render(<SecuritySettings csrf="csrf" />);

        expect(await screen.findByRole("button", { name: "Phoneを削除" })).toBeDisabled();
        await user.type(screen.getByRole("textbox", { name: "新しいパスキーの名前" }), "Laptop");
        await user.click(screen.getByRole("button", { name: "パスキーを追加" }));
        await waitFor(() => expect(finish).toHaveBeenCalledWith("csrf", "ceremony", { id: "new-credential" }));
        expect(start).toHaveBeenCalledWith("csrf", "Laptop");
        expect(createPasskey).toHaveBeenCalledWith({ challenge: "challenge" });

        const phoneName = screen.getByRole("textbox", { name: "Phoneの名前" });
        await user.clear(phoneName);
        await user.type(phoneName, "Main phone");
        await user.click(screen.getAllByRole("button", { name: "名前を保存" })[0]);
        await waitFor(() => expect(rename).toHaveBeenCalledWith("csrf", "phone-key", "Main phone"));

        await user.click(screen.getByRole("button", { name: "Laptopを削除" }));
        expect(remove).not.toHaveBeenCalled();
        await user.click(screen.getByRole("button", { name: "削除する" }));
        await waitFor(() => expect(remove).toHaveBeenCalledWith("csrf", "laptop-key"));
        expect(screen.getByRole("button", { name: "Main phoneを削除" })).toBeDisabled();
    });
});
