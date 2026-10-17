import { expect, test } from "bun:test";
import { decodeBlurhash, isDark, tintColour } from "./blurhash.ts";

test("a hash is drawn as the reference decoder draws it", () => {
	// woltapp/blurhash's own example, decoded by its TypeScript decoder.
	expect(
		Array.from(decodeBlurhash("LEHV6nWB2yk8pyo0adR*.7kCMdnj", 3, 2)),
	).toEqual([
		135, 164, 177, 255, 172, 177, 175, 255, 170, 175, 172, 255, 120, 148, 162,
		255, 150, 128, 125, 255, 150, 135, 125, 255,
	]);
});

test("a logo is dark by its hash's average colour", () => {
	expect(isDark("000000")).toBe(true); // black
	expect(isDark("002~Qp")).toBe(true); // navy, #1a1a59
	expect(isDark("00F$Y7")).toBe(false); // mid grey, #898989
	expect(isDark("00TSUA")).toBe(false); // white
	expect(isDark(undefined)).toBe(false);
});

test("a picture with a colour of its own tints the page", () => {
	// Navy: its hue at the tint's own lightness, not its near-black brightness.
	expect(tintColour("002~Qp")).toBe("hsl(from #1a1a59 h s 35%)");
});

// A real backdrop, Arthur Christmas's: grey through the middle, blue in a
// corner. Its average is #989b98, which is no colour at all.
test("a picture is tinted by its colour where it has one, not by its average", () => {
	expect(
		tintColour("WTHe%r.8o#$kXo%300V[bIR*E1WotTRPM{g3xabaSdoct7W=xaaf"),
	).toBe("hsl(from #859cb4 h s 35%)");
});

test("a picture with no colour of its own tints nothing", () => {
	expect(tintColour("00F$Y7")).toBeUndefined(); // mid grey, #898989
	expect(tintColour("000000")).toBeUndefined(); // black
	expect(tintColour("00TSUA")).toBeUndefined(); // white
	expect(tintColour(undefined)).toBeUndefined();
	expect(tintColour("00T")).toBeUndefined();
});
