import { isSafari, runCanvasWorker } from "@/lib/canvasWorker";

export type CropOptions = { aspect: number; zoom: number; x: number; y: number };
export type ImageResult = { blob: Blob; width: number; height: number };
export type ImageJob = { file: File; crop?: CropOptions; allowJPEG?: boolean };

export function cropRectangle(width: number, height: number, { aspect, zoom, x, y }: CropOptions) {
    if (![width, height, aspect, zoom, x, y].every(Number.isFinite) || width <= 0 || height <= 0 || aspect <= 0 || zoom < 1 || zoom > 4 || x < 0 || x > 1 || y < 0 || y > 1) throw new Error("クロップ範囲が不正です");
    const cropWidth = Math.min(width, height * aspect) / zoom;
    const cropHeight = cropWidth / aspect;
    return { x: (width - cropWidth) * x, y: (height - cropHeight) * y, width: cropWidth, height: cropHeight };
}

export function processProfileImage(file: File, signal: AbortSignal, crop?: CropOptions): Promise<ImageResult> {
    return runCanvasWorker<ImageJob, ImageResult>(() => new Worker(new URL("./profileImage.worker.ts", import.meta.url), { type: "module" }), { file, crop, allowJPEG: isSafari() }, signal);
}
