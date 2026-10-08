import { expect, test } from "bun:test";
import { moved, rail } from "./rows.ts";

test("a row moves to where it is put, the rest keeping their order", () => {
	expect(moved(["a", "b", "c", "d"], 0, 2)).toEqual(["b", "c", "a", "d"]);
	expect(moved(["a", "b", "c", "d"], 3, 0)).toEqual(["d", "a", "b", "c"]);
	// Past either end is the end.
	expect(moved(["a", "b", "c"], 1, 9)).toEqual(["a", "c", "b"]);
});

test("a library's row leads to its wall in the row's order", () => {
	const library = { id: "l-films", name: "Films" };
	const row = { title: "Recently Added in Films", library, items: [] };
	expect(rail({ ...row, kind: "recently_added_films" })).toEqual({
		key: "recently_added_films/l-films",
		href: "/libraries/l-films?sort=added",
	});
	expect(rail({ ...row, kind: "top_rated_unwatched" }).href).toBe(
		"/libraries/l-films?sort=rating&mark=unwatched",
	);
	expect(rail({ kind: "next_up", title: "Next Up", items: [] })).toEqual({
		key: "next_up",
		href: "/home/next_up",
	});
});
