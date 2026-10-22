import { afterEach, beforeEach, expect, jest, test } from "bun:test";
import { settle, settleAtMost, settling } from "./settle.ts";

let reloads: string[][];
let later: ReturnType<typeof settling>;

beforeEach(() => {
	jest.useFakeTimers();
	reloads = [];
	later = settling((keys) => reloads.push(keys));
});

afterEach(() => {
	later.stop();
	jest.useRealTimers();
});

// A change every fifth of a second, as titles are matched during a scan.
function burst(ms: number) {
	for (let at = 0; at < ms; at += 200) {
		later.add(["home", "library:films"]);
		jest.advanceTimersByTime(200);
	}
}

test("a burst reloads its pages once, after it has gone quiet", () => {
	burst(5000);
	expect(reloads).toHaveLength(0);
	jest.advanceTimersByTime(settle);
	expect(reloads).toEqual([["home", "library:films"]]);
});

test("a burst that runs on reloads at most every settleAtMost", () => {
	burst(25_000);
	expect(reloads).toHaveLength(Math.floor(25_000 / settleAtMost));
	jest.advanceTimersByTime(settle);
	expect(reloads).toHaveLength(Math.floor(25_000 / settleAtMost) + 1);
});

test("a change alone reloads once it has settled", () => {
	later.add(["title:1"]);
	jest.advanceTimersByTime(settle - 1);
	expect(reloads).toHaveLength(0);
	jest.advanceTimersByTime(1);
	expect(reloads).toEqual([["title:1"]]);
});
