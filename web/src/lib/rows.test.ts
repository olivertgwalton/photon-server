import { expect, test } from "bun:test";
import { byKind, everyKind, moved, rail } from "./rows.ts";

test("a row moves to where it is put, the rest keeping their order", () => {
	expect(moved(["a", "b", "c", "d"], 0, 2)).toEqual(["b", "c", "a", "d"]);
	expect(moved(["a", "b", "c", "d"], 3, 0)).toEqual(["d", "a", "b", "c"]);
	// Past either end is the end.
	expect(moved(["a", "b", "c"], 1, 9)).toEqual(["a", "c", "b"]);
});

test("a library's row leads to a page of what its rail holds", () => {
	const library = { id: "l-films", name: "Films" };
	const row = { library };
	expect(rail({ ...row, kind: "recently_added_films" })).toEqual({
		key: "recently_added_films/l-films",
		href: "/libraries/l-films/rows/recently_added_films",
	});
	expect(rail({ ...row, kind: "top_rated_unwatched" }).href).toBe(
		"/libraries/l-films/rows/top_rated_unwatched",
	);
	expect(rail({ ...row, kind: "recently_released" }).href).toBe(
		"/libraries/l-films/rows/recently_released",
	);
	expect(rail({ kind: "next_up" })).toEqual({
		key: "next_up",
		href: "/home/next_up",
	});
});

test("favourites of every kind are grouped by kind, music and books after the screen's", () => {
	const card = (id: string, kind: "movie" | "album" | "book") =>
		({ id, kind, title: id, added_at: "" }) as Parameters<
			typeof byKind
		>[0][number];
	const names = {
		movie: "Films",
		album: "Albums",
		book: "Books",
	} as Parameters<typeof byKind>[1];
	const cards = [card("a", "album"), card("b", "book"), card("m", "movie")];
	expect(byKind(cards, names, everyKind).map((g) => g.name)).toEqual([
		"Films",
		"Albums",
		"Books",
	]);
	// A search's results are grouped among the screen's kinds alone.
	expect(byKind(cards, names).map((g) => g.name)).toEqual(["Films"]);
});
