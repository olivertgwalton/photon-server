import { expect, test } from "bun:test";
import { decodeBlurhash } from "./blurhash.ts";

test("a hash is drawn as the reference decoder draws it", () => {
	// woltapp/blurhash's own example, decoded by its TypeScript decoder.
	expect(
		Array.from(decodeBlurhash("LEHV6nWB2yk8pyo0adR*.7kCMdnj", 3, 2)),
	).toEqual([
		135, 164, 177, 255, 172, 177, 175, 255, 170, 175, 172, 255, 120, 148, 162,
		255, 150, 128, 125, 255, 150, 135, 125, 255,
	]);
});
