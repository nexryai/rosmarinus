import { globalCss } from "./lib/css";

const darkTheme = {
    colorScheme: "dark",
    "--page": "#17161a",
    "--panel": "#211f25",
    "--panel-muted": "#29262e",
    "--text": "#f0edf3",
    "--muted": "#aaa5b2",
    "--border": "#39353f",
    "--accent": "#eabb45",
    "--accent-hover": "#f4cd68",
    "--accent-soft": "#443a20",
    "--accent-ink": "#2d2308",
    "--shadow": "0 18px 50px #0000003d",
};

export function GlobalStyles() {
    globalCss({
        ":root": {
            color: "#34323b",
            fontFamily: 'Inter, "Noto Sans JP", ui-sans-serif, system-ui, sans-serif',
            fontSynthesis: "none",
            textRendering: "optimizeLegibility",
            "--page": "#f4f3f0",
            "--panel": "#fff",
            "--panel-muted": "#faf9f7",
            "--text": "#34323b",
            "--muted": "#817d8b",
            "--border": "#e5e2dc",
            "--accent": "#f4bd36",
            "--accent-hover": "#e9a91d",
            "--accent-soft": "#fff4c2",
            "--accent-ink": "#4b3908",
            "--danger": "#c8463a",
            "--shadow": "0 18px 50px #34323b14",
            background: "var(--page)",
        },
        ':root[data-theme="dark"]': darkTheme,
        "@media (prefers-color-scheme: dark)": {
            ':root[data-theme="system"]': darkTheme,
        },
        "*, *::before, *::after": {
            boxSizing: "border-box",
        },
        html: {
            minWidth: "320px",
            background: "var(--page)",
            lineHeight: 1.5,
            WebkitTextSizeAdjust: "100%",
            WebkitTapHighlightColor: "transparent",
        },
        body: {
            minHeight: "100dvh",
            margin: 0,
            color: "var(--text)",
            background: "var(--page)",
            WebkitFontSmoothing: "antialiased",
            MozOsxFontSmoothing: "grayscale",
        },
        "button, input, textarea, select": {
            font: "inherit",
            color: "inherit",
        },
        button: {
            padding: 0,
            border: 0,
            background: "transparent",
            cursor: "pointer",
        },
        "button:disabled": {
            cursor: "not-allowed",
            opacity: 0.55,
        },
        a: {
            color: "inherit",
            textDecoration: "inherit",
            WebkitTapHighlightColor: "transparent",
        },
        "img, svg": {
            display: "block",
            verticalAlign: "middle",
        },
        img: {
            maxWidth: "100%",
            height: "auto",
        },
        "h1, h2, h3, p, dl, dd, blockquote, figure, fieldset": {
            margin: 0,
        },
        fieldset: {
            padding: 0,
            border: 0,
        },
        "::selection": {
            color: "var(--accent-ink)",
            background: "var(--accent)",
        },
        ":focus-visible": {
            outline: "3px solid var(--accent)",
            outlineOffset: "2px",
        },
        ':root[data-reduce-motion="true"] *, :root[data-reduce-motion="true"] *::before, :root[data-reduce-motion="true"] *::after': {
            scrollBehavior: "auto",
            transitionDuration: "0.01ms",
            animationDuration: "0.01ms",
        },
    });
    return null;
}
