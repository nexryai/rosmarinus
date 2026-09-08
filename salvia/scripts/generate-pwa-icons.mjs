import { mkdir, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { deflateSync } from "node:zlib";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const outputDirectory = resolve(root, "public/icons");
const samples = 4;

const crcTable = Array.from({ length: 256 }, (_, value) => {
    let crc = value;
    for (let bit = 0; bit < 8; bit += 1) crc = crc & 1 ? 0xedb88320 ^ (crc >>> 1) : crc >>> 1;
    return crc >>> 0;
});

const chunk = (type, data) => {
    const name = Buffer.from(type);
    let crc = 0xffffffff;
    for (const byte of Buffer.concat([name, data])) crc = crcTable[(crc ^ byte) & 0xff] ^ (crc >>> 8);
    const header = Buffer.alloc(8);
    header.writeUInt32BE(data.length, 0);
    name.copy(header, 4);
    const footer = Buffer.alloc(4);
    footer.writeUInt32BE((crc ^ 0xffffffff) >>> 0);
    return Buffer.concat([header, data, footer]);
};

const insideRoundedRectangle = (x, y, size, radius) => {
    const nearestX = Math.max(radius, Math.min(size - radius, x));
    const nearestY = Math.max(radius, Math.min(size - radius, y));
    return Math.hypot(x - nearestX, y - nearestY) <= radius;
};

const paintCircle = (pixels, size, centerX, centerY, radius, color) => {
    const left = Math.max(0, Math.floor(centerX - radius));
    const right = Math.min(size - 1, Math.ceil(centerX + radius));
    const top = Math.max(0, Math.floor(centerY - radius));
    const bottom = Math.min(size - 1, Math.ceil(centerY + radius));
    for (let y = top; y <= bottom; y += 1) {
        for (let x = left; x <= right; x += 1) {
            if (Math.hypot(x + 0.5 - centerX, y + 0.5 - centerY) > radius) continue;
            const offset = (y * size + x) * 4;
            pixels.set(color, offset);
        }
    }
};

const paintLine = (pixels, size, start, end, width, color) => {
    const steps = Math.ceil(Math.hypot(end[0] - start[0], end[1] - start[1]) * 1.5);
    for (let step = 0; step <= steps; step += 1) {
        const progress = step / steps;
        paintCircle(pixels, size, start[0] + (end[0] - start[0]) * progress, start[1] + (end[1] - start[1]) * progress, width / 2, color);
    }
};

const drawIcon = (targetSize, maskable) => {
    const size = targetSize * samples;
    const pixels = new Uint8Array(size * size * 4);
    const yellow = [244, 189, 54, 255];
    const green = [49, 92, 43, 255];
    const radius = maskable ? 0 : size * 0.23;

    for (let y = 0; y < size; y += 1) {
        for (let x = 0; x < size; x += 1) {
            if (!maskable && !insideRoundedRectangle(x + 0.5, y + 0.5, size, radius)) continue;
            pixels.set(yellow, (y * size + x) * 4);
        }
    }

    const point = (x, y) => [x * size, y * size];
    const stem = [];
    for (let index = 0; index <= 160; index += 1) {
        const t = index / 160;
        const inverse = 1 - t;
        stem.push([(inverse ** 3 * 0.3 + 3 * inverse ** 2 * t * 0.4 + 3 * inverse * t ** 2 * 0.52 + t ** 3 * 0.66) * size, (inverse ** 3 * 0.8 + 3 * inverse ** 2 * t * 0.65 + 3 * inverse * t ** 2 * 0.43 + t ** 3 * 0.2) * size]);
    }
    for (let index = 1; index < stem.length; index += 1) paintLine(pixels, size, stem[index - 1], stem[index], size * 0.047, green);

    const needles = [
        [0.38, 0.7, 0.19, 0.72],
        [0.41, 0.63, 0.23, 0.55],
        [0.46, 0.55, 0.28, 0.43],
        [0.5, 0.47, 0.36, 0.32],
        [0.55, 0.39, 0.46, 0.24],
        [0.59, 0.31, 0.54, 0.17],
        [0.39, 0.66, 0.48, 0.82],
        [0.44, 0.59, 0.62, 0.72],
        [0.48, 0.52, 0.69, 0.6],
        [0.52, 0.44, 0.74, 0.44],
        [0.56, 0.36, 0.76, 0.28],
        [0.61, 0.27, 0.75, 0.16],
    ];
    for (const [startX, startY, endX, endY] of needles) paintLine(pixels, size, point(startX, startY), point(endX, endY), size * 0.042, green);
    paintCircle(pixels, size, size * 0.66, size * 0.2, size * 0.032, green);

    const rows = Buffer.alloc((targetSize * 4 + 1) * targetSize);
    for (let y = 0; y < targetSize; y += 1) {
        rows[y * (targetSize * 4 + 1)] = 0;
        for (let x = 0; x < targetSize; x += 1) {
            const targetOffset = y * (targetSize * 4 + 1) + 1 + x * 4;
            for (let channel = 0; channel < 4; channel += 1) {
                let total = 0;
                for (let sampleY = 0; sampleY < samples; sampleY += 1) {
                    for (let sampleX = 0; sampleX < samples; sampleX += 1) total += pixels[((y * samples + sampleY) * size + x * samples + sampleX) * 4 + channel];
                }
                rows[targetOffset + channel] = Math.round(total / samples ** 2);
            }
        }
    }

    const header = Buffer.alloc(13);
    header.writeUInt32BE(targetSize, 0);
    header.writeUInt32BE(targetSize, 4);
    header.set([8, 6, 0, 0, 0], 8);
    return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", header), chunk("IDAT", deflateSync(rows, { level: 9 })), chunk("IEND", Buffer.alloc(0))]);
};

await mkdir(outputDirectory, { recursive: true });
await Promise.all([
    writeFile(resolve(outputDirectory, "rosemary-180.png"), drawIcon(180, false)),
    writeFile(resolve(outputDirectory, "rosemary-192.png"), drawIcon(192, false)),
    writeFile(resolve(outputDirectory, "rosemary-512.png"), drawIcon(512, false)),
    writeFile(resolve(outputDirectory, "rosemary-maskable-512.png"), drawIcon(512, true)),
]);
