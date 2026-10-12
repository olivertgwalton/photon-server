import { expect, mock, test } from "bun:test";

mock.module("$app/navigation", () => ({ goto: () => Promise.resolve() }));
const { shuffled } = await import("./queue.ts");

test("a shuffle plays every title once", () => {
	const titles = Array.from({ length: 50 }, (_, n) => `t-${n}`);
	const order = shuffled(titles);
	expect(order).not.toBe(titles);
	expect(order.toSorted()).toEqual(titles.toSorted());
});
