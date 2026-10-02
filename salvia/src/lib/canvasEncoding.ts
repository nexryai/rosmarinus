export async function encodeCanvas(canvas: OffscreenCanvas, allowJPEG: boolean, quality: number): Promise<Blob> {
    let blob: Blob;
    try {
        blob = await canvas.convertToBlob({ type: "image/webp", quality });
    } catch (reason) {
        if (!allowJPEG) throw reason;
        blob = await canvas.convertToBlob({ type: "image/jpeg", quality });
    }
    if (blob.type !== "image/webp" && blob.type !== "image/jpeg" && allowJPEG) blob = await canvas.convertToBlob({ type: "image/jpeg", quality });
    if (blob.type !== "image/webp" && !(allowJPEG && blob.type === "image/jpeg")) throw new Error("このブラウザーはWebPエンコードに対応していません");
    return blob;
}
