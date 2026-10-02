import { encodeCanvas } from "@/lib/canvasEncoding";
import type { EncodedImage, PostImageJob, PostImageResult } from "@/lib/postImage";

const scope = globalThis as unknown as { onmessage: (event: MessageEvent<PostImageJob>) => void; postMessage: (result: PostImageResult | { error: string }) => void };
scope.onmessage = async ({ data }) => {
    try {
        if (typeof OffscreenCanvas === "undefined") throw new Error("画像編集に対応したブラウザーをご利用ください");
        const bitmap = await createImageBitmap(data.file, { imageOrientation: "from-image" });
        try {
            const render = async (maxEdge: number, quality: number): Promise<EncodedImage> => {
                const scale = Math.min(1, maxEdge / Math.max(bitmap.width, bitmap.height));
                const width = Math.max(1, Math.round(bitmap.width * scale));
                const height = Math.max(1, Math.round(bitmap.height * scale));
                const canvas = new OffscreenCanvas(width, height);
                const context = canvas.getContext("2d");
                if (!context) throw new Error("Canvasを初期化できませんでした");
                // Redrawing pixels removes source EXIF/GPS metadata even for already-WebP files.
                context.drawImage(bitmap, 0, 0, width, height);
                return { blob: await encodeCanvas(canvas, data.allowJPEG, quality), width, height };
            };
            const image = await render(4096, 0.85);
            const thumbnail = await render(512, 0.86);
            scope.postMessage({ ...image, thumbnail });
        } finally {
            bitmap.close();
        }
    } catch (reason) {
        scope.postMessage({ error: reason instanceof Error ? reason.message : "画像を処理できませんでした" });
    }
};
