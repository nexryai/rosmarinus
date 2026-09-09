import type { HTMLAttributes } from "react";

import { css } from "../lib/css";

const rules = {
    list: css({
        "& > [data-note-list-item]:not(:last-child)": {
            borderBottom: "1px solid var(--border)",
        },
        "@media (width >= 40rem)": {
            "& > [data-note-list-item]:not(:last-child)": {
                borderBottom: 0,
            },
        },
    }),
};

export function NoteList({ children, className = "", ...props }: HTMLAttributes<HTMLElement>) {
    return (
        <section className={[rules.list, className].filter(Boolean).join(" ")} {...props}>
            {children}
        </section>
    );
}

export function NoteListItem(props: HTMLAttributes<HTMLDivElement>) {
    return <div {...props} data-note-list-item="" />;
}
