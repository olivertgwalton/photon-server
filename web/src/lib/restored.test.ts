import { expect, test } from "bun:test";
import { backAfterRestore } from "./restored.js";

function clock() {
	let t = 0;
	return {
		now: () => t,
		wait: async (ms: number) => {
			t += ms;
		},
	};
}

test("the app comes back once a node answers after going away", async () => {
	const answers = [true, false, false, true];
	const c = clock();
	await backAfterRestore(async () => answers.shift() ?? true, c.wait, c.now);
	expect(answers).toEqual([]);
	expect(c.now()).toBe(8_000);
});

test("a restore quicker than the polls is taken as done once a node has answered a while", async () => {
	const c = clock();
	await backAfterRestore(async () => true, c.wait, c.now);
	expect(c.now()).toBe(30_000);
});
