import { expect, test } from "bun:test";
import type { components } from "./api/schema.d.ts";
import { discs, guests } from "./music.ts";

type Card = components["schemas"]["Card"];

const track = (id: string, disc: number | null, n: number): Card => ({
	id,
	kind: "track",
	title: id,
	added_at: "2026-11-03T00:00:00Z",
	disc_number: disc,
	track_number: n,
});

test("an album plays its first disc through before its second", () => {
	const split = discs([
		track("2-1", 2, 1),
		track("1-2", 1, 2),
		track("1-1", 1, 1),
		track("2-2", 2, 2),
	]);
	expect(split.map((d) => d.disc)).toEqual([1, 2]);
	expect(split.map((d) => d.tracks.map((t) => t.id))).toEqual([
		["1-1", "1-2"],
		["2-1", "2-2"],
	]);
});

test("a track no disc names is on the first", () => {
	expect(discs([track("b", null, 2), track("a", 1, 1)])).toEqual([
		{ disc: 1, tracks: [track("a", 1, 1), track("b", null, 2)] },
	]);
});

test("a track names its artists only where they are not the album's", () => {
	const t = { ...track("a", 1, 1), artists: ["The Weeknd"] };
	expect(guests(t, "The Weeknd")).toBeUndefined();
	expect(
		guests({ ...t, artists: ["The Weeknd", "Justice"] }, "The Weeknd"),
	).toBe("The Weeknd, Justice");
});
