import { expect, test } from "bun:test";
import { letteringHeight, readInk } from "./lettering.ts";

// A picture of `width` by `height`, clear but for a block of one colour.
function picture(
	width: number,
	height: number,
	block: { x: number; y: number; w: number; h: number },
	rgb: [number, number, number],
) {
	const pixels = new Uint8ClampedArray(width * height * 4);
	for (let y = block.y; y < block.y + block.h; y++)
		for (let x = block.x; x < block.x + block.w; x++)
			pixels.set([...rgb, 255], (y * width + x) * 4);
	return pixels;
}

test("lettering set on clear margins is the ink alone", () => {
	// A wordmark centred on a fanart.tv-shaped canvas.
	const ink = readInk(
		picture(80, 31, { x: 20, y: 6, w: 40, h: 20 }, [255, 220, 0]),
		80,
		31,
	);
	expect(ink).toMatchObject({ x: 0.25, width: 0.5, aspect: 2, dark: false });
	expect(ink?.y).toBeCloseTo(6 / 31);
});

test("lettering is dark where it would be lost on a dark page, not where it is merely deep", () => {
	const block = { x: 0, y: 0, w: 10, h: 10 };
	const dark = (rgb: [number, number, number]) =>
		readInk(picture(10, 10, block, rgb), 10, 10)?.dark;
	// Navy, maroon, pure blue and charcoal.
	expect(dark([0, 0, 128])).toBe(true);
	expect(dark([128, 0, 0])).toBe(true);
	expect(dark([0, 0, 255])).toBe(true);
	expect(dark([64, 64, 64])).toBe(true);
	// Young Sheldon's red at its deepest, crimson, a mid grey.
	expect(dark([158, 38, 56])).toBe(false);
	expect(dark([200, 30, 60])).toBe(false);
	expect(dark([128, 128, 128])).toBe(false);
});

test("a picture with nothing opaque has no ink", () => {
	expect(readInk(new Uint8ClampedArray(4 * 4 * 4), 4, 4)).toBeUndefined();
});

test("a long wordmark and a stacked block take the same area", () => {
	// Two and a half to one fills the box's height; longer is shorter.
	expect(letteringHeight(2.5)).toBe(1);
	expect(letteringHeight(1)).toBe(1);
	const long = letteringHeight(10);
	expect(long).toBe(0.5);
	expect(long * long * 10).toBeCloseTo(2.5);
});
