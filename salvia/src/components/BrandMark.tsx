import type { CSSProperties } from "react";

export function BrandMark({ style }: { style?: CSSProperties }) {
    return (
        <svg aria-hidden="true" fill="none" style={style} viewBox="0 0 64 64">
            <path d="M19 51C26 42 32 32 42 13" stroke="currentColor" strokeLinecap="round" strokeWidth="3" />
            <path d="m24 45-12 1m14-6-11-5m14 0-11-7m14 2-9-10m12 5-6-10m9 5-3-9M25 42l6 10m-3-14 12 8m-9-13 13 5M33 28h14M36 23l13-5M39 17l9-7" stroke="currentColor" strokeLinecap="round" strokeWidth="2.7" />
            <circle cx="42" cy="13" fill="currentColor" r="2" />
        </svg>
    );
}
