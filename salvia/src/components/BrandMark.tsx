import type { CSSProperties } from "react";

export function BrandMark({ style }: { style?: CSSProperties }) {
    return (
        <svg aria-hidden="true" fill="none" style={style} viewBox="0 0 64 64">
            <path d="M20 52C28 43 31 34 35 25C38 18 42 13 46 10" stroke="currentColor" strokeLinecap="round" strokeWidth="3" />
            <path d="M27 42C22 35 18 32 13 33C16 39 21 42 27 42ZM29 37C34 31 40 31 44 35C40 40 34 41 29 37ZM33 31C30 24 25 21 20 22C22 28 27 32 33 31ZM36 25C40 19 46 17 50 20C47 26 42 29 36 25ZM39 19C38 13 35 9 31 9C31 14 34 19 39 19ZM41 15C44 10 49 7 53 8C51 13 47 17 41 15Z" fill="currentColor" />
        </svg>
    );
}
