import { useState } from "react";

import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";

import { Switch } from "./Switch";

function SwitchHarness({ disabled = false }: { disabled?: boolean }) {
    const [checked, setChecked] = useState(false);
    return <Switch checked={checked} disabled={disabled} label="コンパクト表示" onChange={setChecked} />;
}

describe("Switch", () => {
    afterEach(cleanup);

    it("toggles with pointer and keyboard input", async () => {
        const user = userEvent.setup();
        render(<SwitchHarness />);
        const control = screen.getByRole("switch", { name: "コンパクト表示" });

        expect(control).not.toBeChecked();
        await user.click(screen.getByText("コンパクト表示"));
        expect(control).toBeChecked();
        expect(control).toHaveAttribute("aria-checked", "true");

        control.focus();
        await user.keyboard(" ");
        expect(control).not.toBeChecked();
    });

    it("does not toggle when disabled", async () => {
        const user = userEvent.setup();
        render(<SwitchHarness disabled />);
        const control = screen.getByRole("switch", { name: "コンパクト表示" });

        await user.click(screen.getByText("コンパクト表示"));
        expect(control).not.toBeChecked();
        expect(control).toBeDisabled();
    });
});
