import type { CSSProperties } from "react";

import { css } from "../lib/css";
import type { Note } from "../lib/schema";

type Attachment = Note["attachments"][number];

const styles = {
    root: {
        marginTop: "0.75rem",
        display: "grid",
        gap: "0.5rem",
    },
    gallery: {
        width: "100%",
        display: "grid",
        gap: "0.5rem",
    },
    singleGallery: {
        minHeight: "4rem",
        maxHeight: "min(22.5rem, 50vh)",
    },
    multipleGallery: {
        aspectRatio: "16 / 9",
        overflow: "hidden",
    },
    imageButton: {
        width: "100%",
        height: "100%",
        minWidth: 0,
        minHeight: 0,
        display: "grid",
        placeItems: "center",
        overflow: "hidden",
        borderRadius: "0.5rem",
        background: "var(--panel-muted)",
        cursor: "zoom-in",
    },
    singleImage: {
        width: "100%",
        height: "auto",
        maxHeight: "min(22.5rem, 50vh)",
        objectFit: "contain",
    },
    tiledImage: {
        width: "100%",
        height: "100%",
        objectFit: "contain",
    },
    file: {
        padding: "1rem",
        display: "block",
        border: "1px solid var(--border)",
        borderRadius: "0.75rem",
        fontSize: "0.875rem",
        textDecoration: "underline",
    },
} satisfies Record<string, CSSProperties>;

const rules = {
    imageButton: css({
        "&:hover img": {
            transform: "scale(1.015)",
        },
        "& img": {
            transition: "transform 160ms cubic-bezier(.2,.8,.2,1)",
        },
    }),
};

const galleryLayout = (count: number): CSSProperties => {
    if (count === 1) return styles.singleGallery;
    if (count === 2) return { ...styles.multipleGallery, gridTemplateColumns: "1fr 1fr", gridTemplateRows: "1fr" };
    if (count === 3) return { ...styles.multipleGallery, gridTemplateColumns: "1fr 0.5fr", gridTemplateRows: "1fr 1fr" };
    if (count === 4) return { ...styles.multipleGallery, gridTemplateColumns: "1fr 1fr", gridTemplateRows: "1fr 1fr" };
    return { gridTemplateColumns: "1fr 1fr" };
};

const imageLayout = (count: number, index: number): CSSProperties => {
    if (count === 3 && index === 0) return { gridRow: "1 / 3" };
    if (count > 4) return { aspectRatio: "16 / 9" };
    return {};
};

export function NoteMedia({ attachments, onOpenImage }: { attachments: Attachment[]; onOpenImage: (index: number) => void }) {
    const images = attachments.filter((attachment) => attachment.media_type?.startsWith("image/"));
    const files = attachments.filter((attachment) => !attachment.media_type?.startsWith("image/"));

    return (
        <div style={styles.root}>
            {images.length > 0 && (
                <div data-image-count={images.length} style={{ ...styles.gallery, ...galleryLayout(images.length) }}>
                    {images.map((attachment, index) => (
                        <button aria-label={`画像を表示: ${attachment.name || "添付画像"}`} className={rules.imageButton} key={attachment.url} onClick={() => onOpenImage(index)} style={{ ...styles.imageButton, ...imageLayout(images.length, index) }} type="button">
                            <img alt={attachment.name || "添付画像"} loading="lazy" referrerPolicy="no-referrer" src={attachment.thumbnail_url ?? attachment.url} style={images.length === 1 ? styles.singleImage : styles.tiledImage} />
                        </button>
                    ))}
                </div>
            )}
            {files.map((attachment) => (
                <a href={attachment.url} key={attachment.url} rel="noreferrer" style={styles.file} target="_blank">
                    {attachment.name || "添付ファイル"}
                </a>
            ))}
        </div>
    );
}
