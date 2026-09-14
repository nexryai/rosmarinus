import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AuthScreen } from "./AuthScreen";

describe("AuthScreen", () => {
    afterEach(cleanup);

    it("explains that authentication is required before showing private pages", () => {
        render(<AuthScreen mode="login" onAuthenticated={async () => undefined} />);

        expect(screen.getByText("このページを見るにはパスキーでのログインが必要です")).toBeInTheDocument();
        expect(screen.getByRole("button", { name: /パスキーでログイン/ })).toBeInTheDocument();
    });
});
