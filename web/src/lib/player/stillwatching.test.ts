import { expect, test } from "bun:test";
import { isDue, runAfter } from "./stillwatching.ts";

test("asks two minutes into the third episode in a row", () => {
	const third = runAfter(runAfter(1, "itself"), "itself");
	expect(third).toBe(3);
	expect(isDue(third, 119)).toBe(false);
	expect(isDue(third, 120)).toBe(true);
});

test("does not ask on the second episode however long it runs", () => {
	expect(isDue(2, 50 * 60)).toBe(false);
});

test("an episode the reader chose starts the run again", () => {
	expect(runAfter(5, "reader")).toBe(1);
	expect(isDue(runAfter(5, "reader"), 600)).toBe(false);
});
