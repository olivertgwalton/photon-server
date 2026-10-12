import { expect, test } from "bun:test";
import {
	cleared,
	filterCount,
	letterOffset,
	ruleQuery,
	smartRule,
	toggled,
	wallQuery,
	wallSearch,
} from "./wall.ts";

test("a narrowed wall survives the trip through its address", () => {
	const query = {
		sort: "rating" as const,
		order: "asc" as const,
		rating_site: "tmdb" as const,
		genre: ["Drama", "Comedy"],
		year: [1999, 2001],
		mark: ["unwatched" as const],
		min_rating: 70,
	};
	const search = wallSearch(query);
	expect(search).toContain("genre=Drama%2CComedy");
	expect(wallQuery(new URLSearchParams(search))).toEqual(query);
});

test("what the server would refuse is dropped from an address, not sent", () => {
	expect(
		wallQuery(
			new URLSearchParams(
				"sort=shuffle&order=up&mark=seen&rating_site=imdb2&min_rating=500&year=soon&person=bob&resolution=8k",
			),
		),
	).toEqual({});
});

test("a filter value is turned on and off again", () => {
	const on = toggled({}, "genre", "Drama");
	expect(on.genre).toEqual(["Drama"]);
	expect(filterCount(on)).toBe(1);
	expect(toggled(on, "genre", "Drama").genre).toBeUndefined();
});

test("clearing filters keeps what the wall holds and the order the reader chose", () => {
	expect(
		cleared({
			kind: "episodes",
			sort: "added",
			order: "asc",
			genre: ["Drama"],
			min_rating: 60,
		}),
	).toEqual({ kind: "episodes", sort: "added", order: "asc" });
});

test("a wall of episodes survives the trip through its address", () => {
	const query = { kind: "episodes" as const, sort: "released" as const };
	expect(wallQuery(new URLSearchParams(wallSearch(query)))).toEqual(query);
	expect(wallQuery(new URLSearchParams("kind=seasons"))).toEqual({});
});

test("a letter's place is the count of every title before it", () => {
	const letters = [
		{ letter: "#", count: 2 },
		{ letter: "A", count: 5 },
		{ letter: "C", count: 3 },
	];
	expect(letterOffset(letters, "#", "asc")).toBe(0);
	expect(letterOffset(letters, "C", "asc")).toBe(7);
	expect(letterOffset(letters, "A", "desc")).toBe(3);
});

test("a narrowed wall is kept as a smart collection, and opens again as it was", () => {
	const query = {
		sort: "added" as const,
		order: "desc" as const,
		genre: ["Comedy"],
		year: [1993],
		resolution: ["4k" as const],
		min_rating: 70,
		rating_site: "imdb" as const,
	};
	const rule = smartRule(query);
	expect(rule?.filter.genres).toEqual(["Comedy"]);
	expect(rule && ruleQuery(rule)).toEqual(query);
});

test("a wall of a profile's own marks or plays is no smart collection", () => {
	expect(smartRule({ mark: ["unwatched"] })).toBeUndefined();
	expect(smartRule({ sort: "played" })).toBeUndefined();
});

test("a wall of episodes is no smart collection, which holds titles", () => {
	expect(smartRule({ kind: "episodes", genre: ["Drama"] })).toBeUndefined();
});
