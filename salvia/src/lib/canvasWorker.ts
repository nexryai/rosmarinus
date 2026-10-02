export function isSafari() {
    return /Safari\//.test(navigator.userAgent) && !/Chrome|Chromium|CriOS|FxiOS|EdgiOS|Android/.test(navigator.userAgent);
}

export function runCanvasWorker<Input, Output>(createWorker: () => Worker, input: Input, signal: AbortSignal): Promise<Output> {
    return new Promise((resolve, reject) => {
        if (signal.aborted) return reject(new DOMException("Aborted", "AbortError"));
        if (typeof Worker === "undefined") return reject(new Error("画像編集に対応したブラウザーをご利用ください"));
        const worker = createWorker();
        const finish = () => {
            worker.terminate();
            signal.removeEventListener("abort", abort);
        };
        const abort = () => {
            finish();
            reject(new DOMException("Aborted", "AbortError"));
        };
        const fail = () => {
            finish();
            reject(new Error("画像を処理できませんでした"));
        };
        signal.addEventListener("abort", abort, { once: true });
        worker.onmessage = (event: MessageEvent<Output & { error?: string }>) => {
            finish();
            if (event.data.error) reject(new Error(event.data.error));
            else resolve(event.data);
        };
        worker.onerror = fail;
        worker.onmessageerror = fail;
        try {
            worker.postMessage(input);
        } catch (reason) {
            finish();
            reject(reason);
        }
    });
}
