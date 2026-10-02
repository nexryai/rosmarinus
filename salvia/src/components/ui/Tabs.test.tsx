import { useState } from "react";

import { IconBell, IconUserCircle } from "@tabler/icons-react";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import { Tabs } from "@/components/ui/Tabs";

afterEach(cleanup);

describe("Tabs", () => {
    it("moves selection and focus with the arrow keys", async () => {
        const user = userEvent.setup();
        function ExampleTabs() {
            const [value, setValue] = useState("actor");
            return (
                <Tabs
                    ariaLabel="通知の範囲"
                    items={[
                        { value: "actor", label: "このActor" },
                        { value: "account", label: "すべてのActor" },
                    ]}
                    onChange={setValue}
                    value={value}
                />
            );
        }

        render(<ExampleTabs />);
        const actorTab = screen.getByRole("tab", { name: "このActor" });
        const accountTab = screen.getByRole("tab", { name: "すべてのActor" });

        expect(actorTab).toHaveAttribute("aria-selected", "true");
        expect(actorTab).toHaveAttribute("tabindex", "0");
        expect(accountTab).toHaveAttribute("aria-selected", "false");

        actorTab.focus();
        await user.keyboard("{ArrowRight}");

        expect(accountTab).toHaveFocus();
        expect(accountTab).toHaveAttribute("aria-selected", "true");
        expect(actorTab).toHaveAttribute("tabindex", "-1");
    });

    it("shows required underline icons without changing tab names", () => {
        render(
            <Tabs
                ariaLabel="通知の範囲"
                items={[
                    { value: "actor", label: "このActor", icon: <IconUserCircle /> },
                    { value: "all", label: "すべて", icon: <IconBell /> },
                ]}
                onChange={vi.fn()}
                value="actor"
                variant="underline"
            />,
        );
        const actorTab = screen.getByRole("tab", { name: "このActor" });
        expect(actorTab.querySelector("svg")).toBeInTheDocument();
        expect(actorTab.querySelector('[aria-hidden="true"]')).toBeInTheDocument();
        expect(screen.getByRole("tab", { name: "すべて" }).querySelector("svg")).toBeInTheDocument();
    });
});
