import type { CSSProperties } from "react";

const acceptedImageTypes = "image/jpeg,image/png,image/gif,image/webp";

const styles = {
    hidden: {
        position: "absolute",
        width: 1,
        height: 1,
        margin: -1,
        padding: 0,
        overflow: "hidden",
        clipPath: "inset(50%)",
        whiteSpace: "nowrap",
    } satisfies CSSProperties,
};

export function ImageFileInput({
    disabled,
    hidden = false,
    id,
    maxFiles = 1,
    multiple = false,
    onSelect,
    required = false,
    resetAfterSelect = true,
}: {
    disabled?: boolean;
    hidden?: boolean;
    id?: string;
    maxFiles?: number;
    multiple?: boolean;
    onSelect: (files: File[]) => void;
    required?: boolean;
    resetAfterSelect?: boolean;
}) {
    return (
        <input
            accept={acceptedImageTypes}
            disabled={disabled}
            id={id}
            multiple={multiple}
            onChange={(event) => {
                const files = Array.from(event.currentTarget.files ?? []).slice(0, maxFiles);
                if (resetAfterSelect) event.currentTarget.value = "";
                onSelect(files);
            }}
            required={required}
            style={hidden ? styles.hidden : undefined}
            type="file"
        />
    );
}
