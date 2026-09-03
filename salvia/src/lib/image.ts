export type CanvasThumbnail = { blob: Blob; height: number; url: string; width: number };

export async function createCanvasThumbnail(file: File, maxEdge = 512, quality = 0.86): Promise<CanvasThumbnail> {
    if (!file.type.startsWith("image/")) throw new Error("画像ファイルを選択してください");
    if (maxEdge < 1) throw new Error("サムネイルサイズが不正です");
    const bitmap = await createImageBitmap(file, { imageOrientation: "from-image" });
    try {
        const scale = Math.min(1, maxEdge / Math.max(bitmap.width, bitmap.height));
        const width = Math.max(1, Math.round(bitmap.width * scale));
        const height = Math.max(1, Math.round(bitmap.height * scale));
        const canvas = document.createElement("canvas");
        canvas.width = width;
        canvas.height = height;
        const context = canvas.getContext("2d");
        if (!context) throw new Error("Canvasを初期化できませんでした");
        context.drawImage(bitmap, 0, 0, width, height);
        const blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob((value) => (value ? resolve(value) : reject(new Error("サムネイルを生成できませんでした"))), "image/webp", quality));
        return { blob, height, url: URL.createObjectURL(blob), width };
    } finally {
        bitmap.close();
    }
}

export const revokeCanvasThumbnail = (thumbnail: CanvasThumbnail | null) => {
    if (thumbnail) URL.revokeObjectURL(thumbnail.url);
};
