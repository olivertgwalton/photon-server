import { artworkURL } from "./artwork.js";

// What a title's lettering is, read off its pixels as the apps read it: where
// its ink lies (fractions of each side, from the top left), the ink's width
// over its height, and whether it is dark enough to be lost on a dark page.
// A server's logo is often set on clear margins, which are not the lettering.
export type Ink = {
	x: number;
	y: number;
	width: number;
	height: number;
	aspect: number;
	dark: boolean;
};

// Fainter than this is a soft shadow's tail or a stray speck, not ink.
const inkAlpha = 16;
// Lettering is dark when it stands out from a dark page by less than 2.5:1:
// its light, averaged over its ink, below this share of white's. Navy, maroon,
// pure blue and charcoal marks are; a saturated red, which a plain average of
// its sRGB values took for dark, is not.
const darkLuminance = 0.075;

// The light an sRGB channel stands for, of 0 to 1.
function linear(channel: number): number {
	const c = channel / 255;
	return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

// From straight (not premultiplied) RGBA, as a canvas gives it.
export function readInk(
	pixels: Uint8ClampedArray,
	width: number,
	height: number,
): Ink | undefined {
	let [minX, minY, maxX, maxY] = [width, height, -1, -1];
	let [weight, luminance] = [0, 0];
	for (let y = 0; y < height; y++) {
		for (let x = 0; x < width; x++) {
			const i = (y * width + x) * 4;
			const a = pixels[i + 3];
			if (!a) continue;
			const alpha = a / 255;
			luminance +=
				(0.2126 * linear(pixels[i]) +
					0.7152 * linear(pixels[i + 1]) +
					0.0722 * linear(pixels[i + 2])) *
				alpha;
			weight += alpha;
			if (a <= inkAlpha) continue;
			minX = Math.min(minX, x);
			maxX = Math.max(maxX, x);
			minY = Math.min(minY, y);
			maxY = Math.max(maxY, y);
		}
	}
	if (maxX < 0) return undefined;
	const inkWidth = maxX - minX + 1;
	const inkHeight = maxY - minY + 1;
	return {
		x: minX / width,
		y: minY / height,
		width: inkWidth / width,
		height: inkHeight / height,
		aspect: inkWidth / inkHeight,
		dark: luminance / weight < darkLuminance,
	};
}

// Lettering of every shape is given the area this shape takes at the full
// height of its box, so a long wordmark and a stacked block read at one
// weight: anything squarer stops at the box's height, anything longer is
// shorter. As a share of the box's height, before the box's width caps it.
const fillingAspect = 2.5;
export function letteringHeight(aspect: number): number {
	return Math.min(1, Math.sqrt(fillingAspect / aspect));
}

// Lettering is read small: its edges, not its detail, are wanted.
const sample = 128;
const read = new Map<string, Promise<Ink | undefined>>();

// The lettering's ink, read once a picture however often it is drawn.
export function inkOf(id: string): Promise<Ink | undefined> {
	let ink = read.get(id);
	if (!ink) {
		ink = measure(id);
		read.set(id, ink);
	}
	return ink;
}

async function measure(id: string): Promise<Ink | undefined> {
	const img = new Image();
	img.src = artworkURL(id, 320);
	try {
		await img.decode();
	} catch {
		return undefined;
	}
	// An SVG drawn by its viewBox alone has no size to read.
	if (!img.naturalWidth || !img.naturalHeight) return undefined;
	const scale = sample / Math.max(img.naturalWidth, img.naturalHeight);
	const width = Math.max(1, Math.round(img.naturalWidth * scale));
	const height = Math.max(1, Math.round(img.naturalHeight * scale));
	const canvas = new OffscreenCanvas(width, height);
	const context = canvas.getContext("2d", { willReadFrequently: true });
	if (!context) return undefined;
	context.drawImage(img, 0, 0, width, height);
	return readInk(context.getImageData(0, 0, width, height).data, width, height);
}
