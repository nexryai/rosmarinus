import { encodeCanvas } from "@/lib/canvasEncoding";
import { cropRectangle, type ImageJob, type ImageResult } from "@/lib/profileImage";

const scope = globalThis as unknown as { onmessage: (event: MessageEvent<ImageJob>) => void; postMessage: (result: ImageResult | { error: string }) => void };
scope.onmessage = async ({ data }) => {
    try {
        if (typeof OffscreenCanvas === "undefined") throw new Error("画像編集に対応したブラウザーをご利用ください");
        const bitmap = await createImageBitmap(data.file, { imageOrientation: "from-image" });
        try {
            const rect = data.crop ? cropRectangle(bitmap.width, bitmap.height, data.crop) : { x: 0, y: 0, width: bitmap.width, height: bitmap.height };
            // Bound both preview and output allocation; only the worker decodes the original.
            const scale = Math.min(1, (data.crop ? 2048 : 1024) / Math.max(rect.width, rect.height));
            const width = Math.max(1, Math.round(rect.width * scale));
            const height = Math.max(1, Math.round(rect.height * scale));
            const canvas = new OffscreenCanvas(width, height);
            const context = canvas.getContext("2d");
            if (!context) throw new Error("Canvasを初期化できませんでした");
            context.drawImage(bitmap, rect.x, rect.y, rect.width, rect.height, 0, 0, width, height);
            const blob = await encodeCanvas(canvas, data.allowJPEG ?? false, 0.9);
            scope.postMessage({ blob, width: data.crop ? width : bitmap.width, height: data.crop ? height : bitmap.height });
        } finally {
            bitmap.close();
        }
    } catch (reason) {
        scope.postMessage({ error: reason instanceof Error ? reason.message : "画像を処理できませんでした" });
    }
};
