import { expect, test } from "bun:test";
import type { components } from "./api/schema.d.ts";
import { chaptersOf, formatOf, seriesLine } from "./books.ts";

type Version = components["schemas"]["VersionPage"];

const version = (over: Partial<Version>): Version => ({
	id: "v",
	container: "mp3",
	display_title: "",
	duration_ms: 0,
	files: [],
	parts: 1,
	size_bytes: 0,
	streams: [],
	...over,
});

test("an ebook is known by its container, anything else is listened to", () => {
	expect(formatOf(version({ container: "epub" }))).toBe("epub");
	expect(formatOf(version({ container: "cbz" }))).toBe("cbz");
	expect(formatOf(version({ container: "mp3" }))).toBe("audiobook");
	expect(formatOf(version({ container: "mov,mp4,m4a,3gp,3g2,mj2" }))).toBe(
		"audiobook",
	);
});

test("a book names its place in its series, which may fall between two", () => {
	expect(seriesLine("Discworld", 1.5)).toBe("Discworld, book 1.5");
	expect(seriesLine("Discworld", null)).toBe("Discworld");
	expect(seriesLine(undefined, 3)).toBeUndefined();
});

test("an audiobook in several files without chapters has one for each file", () => {
	const files = [0, 1, 2].map((index) => ({
		id: `f${index}`,
		index,
		offset_ms: index * 1000,
		duration_ms: 1000,
		size_bytes: 1,
	}));
	expect(chaptersOf(version({ files }))).toEqual([
		{ start_ms: 0, end_ms: 1000, title: "Part 1" },
		{ start_ms: 1000, end_ms: 2000, title: "Part 2" },
		{ start_ms: 2000, end_ms: 3000, title: "Part 3" },
	]);
	// Its own chapters are kept.
	const chapters = [{ start_ms: 0, end_ms: 3000, title: "Prologue" }];
	expect(chaptersOf(version({ files, chapters }))).toEqual(chapters);
	// One file and none is none.
	expect(chaptersOf(version({ files: files.slice(0, 1) }))).toEqual([]);
});
