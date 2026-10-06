import { expect, test } from "bun:test";
import { thumbnail } from "./trickplay.ts";

const sheet = {
	width: 320,
	height: 180,
	interval_ms: 10_000,
	columns: 10,
	rows: 10,
	sheets: 2,
	thumbnails: 150,
};
const parts = [
	{ ...sheet, part_id: "a", offset_ms: 0 },
	{ ...sheet, part_id: "b", offset_ms: 1_500_000, thumbnails: 12, sheets: 1 },
];

test("a moment finds its picture in its sheet's grid", () => {
	expect(thumbnail(parts, 1_234_000)).toEqual({
		url: "/api/v1/parts/a/trickplay/1",
		x: 3 * 320,
		y: 2 * 180,
		width: 320,
		height: 180,
		sheetWidth: 3200,
		sheetHeight: 1800,
	});
});

test("a moment in a later part is timed from where that part starts", () => {
	expect(thumbnail(parts, 1_525_000)).toMatchObject({
		url: "/api/v1/parts/b/trickplay/0",
		x: 2 * 320,
		y: 0,
	});
});

test("past the last thumbnail is the last thumbnail", () => {
	expect(thumbnail(parts, 9_999_999)).toMatchObject({
		x: (11 % 10) * 320,
		y: 180,
	});
});

test("no sheets, no thumbnail", () => {
	expect(thumbnail([], 5_000)).toBeUndefined();
});
