import { api } from "./api";

export type UploadDimensions = { width: number; height: number };
export type UploadedImage = { id: string; url: string; preview_url: string };

export async function uploadImage(csrf: string, actorID: string, file: File, dimensions: UploadDimensions, intentKey: string = crypto.randomUUID()): Promise<UploadedImage> {
    const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", await file.arrayBuffer())))
        .map((value) => value.toString(16).padStart(2, "0"))
        .join("");
    const pending = await api.prepareMediaUpload(
        csrf,
        actorID,
        {
            name: file.name,
            content_type: file.type,
            size: file.size,
            sha256: digest,
            width: dimensions.width,
            height: dimensions.height,
        },
        intentKey,
    );
    if (pending.state !== "ready") {
        const headers = new Headers(pending.upload_headers);
        headers.delete("content-length");
        const response = await fetch(pending.upload_url, { method: "PUT", body: file, headers });
        if (!response.ok) {
            await api.deleteMedia(csrf, actorID, pending.id).catch(() => undefined);
            throw new Error(`画像をアップロードできませんでした (${response.status})`);
        }
    }
    try {
        const uploaded = await api.completeMediaUpload(csrf, actorID, pending.id);
        return { id: uploaded.id, url: uploaded.url, preview_url: uploaded.url };
    } catch (reason) {
        await api.deleteMedia(csrf, actorID, pending.id).catch(() => undefined);
        throw reason;
    }
}
