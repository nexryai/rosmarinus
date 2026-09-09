import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { NoteList, NoteListItem } from "./NoteList";

describe("NoteList", () => {
    afterEach(cleanup);

    it("divides flat mobile notes and removes the dividers around desktop cards", () => {
        const { container } = render(
            <NoteList>
                <NoteListItem>最初のノート</NoteListItem>
                <NoteListItem>次のノート</NoteListItem>
            </NoteList>,
        );

        const list = container.querySelector("section") as HTMLElement;
        const css = document.head.querySelector("style[data-salvia-css]")?.textContent ?? "";
        expect(css).toContain(`.${list.className} > [data-note-list-item]:not(:last-child){border-bottom:1px solid var(--border);}`);
        expect(css).toContain(`@media (width >= 40rem){.${list.className} > [data-note-list-item]:not(:last-child){border-bottom:0;}}`);
    });
});
