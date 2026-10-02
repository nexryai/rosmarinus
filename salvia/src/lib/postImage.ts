import { isSafari, runCanvasWorker } from "@/lib/canvasWorker";

export type EncodedImage = { blob: Blob; width: number; height: number };
export type PostImageResult = EncodedImage & { thumbnail: EncodedImage };
export type PostImageJob = { file: File; allowJPEG: boolean };
export type PreparedPostImage = { file: File; width: number; height: number; thumbnailURL: string };

export async function preparePostImage(file: File, signal: AbortSignal): Promise<PreparedPostImage> {
    if (!["image/jpeg", "image/png", "image/gif", "image/webp"].includes(file.type)) throw new Error("画像ファイルを選択してください");
    if (file.size > 32 * 1024 * 1024) throw new Error("32MB以下の画像を選択してください");
    const allowJPEG = isSafari();
    const result = await runCanvasWorker<PostImageJob, PostImageResult>(() => new Worker(new URL("./postImage.worker.ts", import.meta.url), { type: "module" }), { file, allowJPEG }, signal);
    if (signal.aborted) throw new DOMException("Aborted", "AbortError");
    if (result.blob.type !== "image/webp" && !(allowJPEG && result.blob.type === "image/jpeg")) throw new Error("画像を安全にエンコードできませんでした");
    // Only freshly encoded bytes and a generic filename cross the upload boundary.
    const encoded = new File([result.blob], `image.${result.blob.type === "image/webp" ? "webp" : "jpg"}`, { type: result.blob.type, lastModified: 0 });
    return { file: encoded, width: result.width, height: result.height, thumbnailURL: URL.createObjectURL(result.thumbnail.blob) };
}
