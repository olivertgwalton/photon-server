import { expect, test } from "bun:test";
import { moved } from "./rows.ts";

test("a row moves to where it is put, the rest keeping their order", () => {
	expect(moved(["a", "b", "c", "d"], 0, 2)).toEqual(["b", "c", "a", "d"]);
	expect(moved(["a", "b", "c", "d"], 3, 0)).toEqual(["d", "a", "b", "c"]);
	// Past either end is the end.
	expect(moved(["a", "b", "c"], 1, 9)).toEqual(["a", "c", "b"]);
});
