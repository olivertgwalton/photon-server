import { expect, test } from "bun:test";
import { shuffled } from "./shuffle.ts";

test("a shuffle plays every title once", () => {
	const titles = Array.from({ length: 50 }, (_, n) => `t-${n}`);
	const order = shuffled(titles);
	expect(order).not.toBe(titles);
	expect(order.toSorted()).toEqual(titles.toSorted());
});
