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
    return new Promise((resolve, reject) => {
        if (signal.aborted) return reject(new DOMException("Aborted", "AbortError"));
        if (typeof Worker === "undefined") return reject(new Error("画像編集に対応したブラウザーをご利用ください"));
        const worker = new Worker(new URL("./profileImage.worker.ts", import.meta.url), { type: "module" });
        const finish = () => {
            worker.terminate();
            signal.removeEventListener("abort", abort);
        };
        const abort = () => {
            finish();
            reject(new DOMException("Aborted", "AbortError"));
        };
        signal.addEventListener("abort", abort, { once: true });
        worker.onmessage = (event: MessageEvent<ImageResult & { error?: string }>) => {
            finish();
            if (event.data.error) reject(new Error(event.data.error));
            else resolve(event.data);
        };
        worker.onerror = () => {
            finish();
            reject(new Error("画像を処理できませんでした"));
        };
        worker.postMessage({ file, crop, allowJPEG: /Safari\//.test(navigator.userAgent) && !/Chrome|Chromium|CriOS|FxiOS|EdgiOS|Android/.test(navigator.userAgent) } satisfies ImageJob);
    });
}
