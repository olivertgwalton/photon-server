import { expect, test } from "bun:test";
import { decodeBlurhash, isDark } from "./blurhash.ts";

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
