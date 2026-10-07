// A BlurHash (https://blurha.sh) is a picture's few strongest waves of colour,
// which the server takes as it keeps the picture. Drawn under the picture, it
// stands in while the picture loads.

const digits =
	"0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~";

const base83 = (s: string) =>
	[...s].reduce((v, c) => v * 83 + digits.indexOf(c), 0);

const toLinear = (v: number) => {
	const f = v / 255;
	return f <= 0.04045 ? f / 12.92 : ((f + 0.055) / 1.055) ** 2.4;
};

const toSRGB = (v: number) => {
	const f = Math.max(0, Math.min(1, v));
	return Math.trunc(
		f <= 0.0031308
			? f * 12.92 * 255 + 0.5
			: (1.055 * f ** (1 / 2.4) - 0.055) * 255 + 0.5,
	);
};

const signPow = (v: number, e: number) => Math.sign(v) * Math.abs(v) ** e;

// The RGBA pixels of a hash drawn width by height, as the reference decoder
// draws them.
export function decodeBlurhash(hash: string, width: number, height: number) {
	const size = base83(hash[0]);
	const cx = (size % 9) + 1;
	const cy = Math.floor(size / 9) + 1;
	const max = (base83(hash[1]) + 1) / 166;
	const colours: number[][] = [];
	for (let i = 0; i < cx * cy; i++) {
		if (i === 0) {
			const v = base83(hash.slice(2, 6));
			colours.push([v >> 16, (v >> 8) & 255, v & 255].map(toLinear));
		} else {
			const v = base83(hash.slice(4 + i * 2, 6 + i * 2));
			colours.push(
				[Math.floor(v / 361), Math.floor(v / 19) % 19, v % 19].map(
					(q) => signPow((q - 9) / 9, 2) * max,
				),
			);
		}
	}
	const pixels = new Uint8ClampedArray(width * height * 4);
	for (let y = 0; y < height; y++) {
		for (let x = 0; x < width; x++) {
			const rgb = [0, 0, 0];
			for (let j = 0; j < cy; j++) {
				for (let i = 0; i < cx; i++) {
					const basis =
						Math.cos((Math.PI * x * i) / width) *
						Math.cos((Math.PI * y * j) / height);
					const c = colours[i + j * cx];
					for (let k = 0; k < 3; k++) rgb[k] += c[k] * basis;
				}
			}
			const at = 4 * (x + y * width);
			for (let k = 0; k < 3; k++) pixels[at + k] = toSRGB(rgb[k]);
			pixels[at + 3] = 255;
		}
	}
	return pixels;
}

// The blur is drawn this small and stretched: it has no detail to lose.
const drawn = 32;

// Each hash is drawn once: a wall redraws its cards as it scrolls, and a
// title shows on many rows. Each is a couple of kilobytes, so the oldest go
// past a few thousand.
const drawnStyles = new Map<string, string>();
const drawnLimit = 4_000;

// The style that draws a hash behind an <img>, stretched to its box, so the
// picture loads over it with nothing moving.
export function blurStyle(hash: string | undefined): string | undefined {
	if (!hash) return undefined;
	const drawnStyle = drawnStyles.get(hash);
	if (drawnStyle) return drawnStyle;
	const style = drawBlur(hash);
	if (style) drawnStyles.set(hash, style);
	if (drawnStyles.size > drawnLimit)
		drawnStyles.delete(drawnStyles.keys().next().value as string);
	return style;
}

function drawBlur(hash: string): string | undefined {
	const canvas = document.createElement("canvas");
	canvas.width = canvas.height = drawn;
	const context = canvas.getContext("2d");
	if (!context) return undefined;
	context.putImageData(
		new ImageData(decodeBlurhash(hash, drawn, drawn), drawn),
		0,
		0,
	);
	return `background: url(${canvas.toDataURL()}) 0 0 / 100% 100%`;
}
