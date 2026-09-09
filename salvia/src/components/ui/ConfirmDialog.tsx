import type { CSSProperties, PropsWithChildren } from "react";

import { IconAlertTriangle } from "@tabler/icons-react";

import { Button, Modal } from "../ui";

const styles = {
    content: {
        paddingRight: "2.75rem",
        display: "grid",
        justifyItems: "start",
        textAlign: "left",
    },
    icon: {
        width: "2.75rem",
        height: "2.75rem",
        marginBottom: "1rem",
        display: "grid",
        placeItems: "center",
        borderRadius: "9999px",
        color: "var(--danger)",
        background: "var(--panel-muted)",
    },
    iconSvg: {
        width: "1.5rem",
        height: "1.5rem",
    },
    title: {
        margin: 0,
        fontSize: "1.25rem",
        lineHeight: 1.4,
        fontWeight: 900,
        textAlign: "left",
    },
    body: {
        marginTop: "0.5rem",
        color: "var(--muted)",
        lineHeight: 1.65,
        textAlign: "left",
    },
    actions: {
        marginTop: "1.5rem",
        display: "flex",
        justifyContent: "flex-end",
        gap: "0.625rem",
    },
} satisfies Record<string, CSSProperties>;

export function ConfirmDialog({
    busy = false,
    cancelLabel = "キャンセル",
    children,
    confirmLabel,
    onCancel,
    onConfirm,
    title,
}: PropsWithChildren<{
    busy?: boolean;
    cancelLabel?: string;
    confirmLabel: string;
    onCancel: () => void;
    onConfirm: () => void;
    title: string;
}>) {
    return (
        <Modal label={title} onClose={() => !busy && onCancel()}>
            <div data-confirm-dialog-part="content" style={styles.content}>
                <span aria-label="警告" role="img" style={styles.icon}>
                    <IconAlertTriangle aria-hidden="true" style={styles.iconSvg} />
                </span>
                <h2 style={styles.title}>{title}</h2>
                <div style={styles.body}>{children}</div>
            </div>
            <footer data-confirm-dialog-part="actions" style={styles.actions}>
                <Button disabled={busy} onClick={onCancel} variant="ghost">
                    {cancelLabel}
                </Button>
                <Button disabled={busy} onClick={onConfirm} variant="danger">
                    {confirmLabel}
                </Button>
            </footer>
        </Modal>
    );
}
