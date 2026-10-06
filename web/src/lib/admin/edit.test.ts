import { expect, test } from "bun:test";
import type { components } from "#lib/api/schema.js";
import { editOf, markersOf } from "./edit";

const film: components["schemas"]["TitlePage"] = {
	id: "t-film",
	kind: "movie",
	title: "Quiet Hours",
	added_at: "2026-10-01T20:00:00Z",
	year: 2018,
	genres: ["Drama"],
	overview: "Two nights.",
};

function form(fields: [string, string][]) {
	const f = new FormData();
	for (const [k, v] of fields) f.append(k, v);
	return f;
}

const as = (changes: Record<string, string>) =>
	form(
		Object.entries({
			title: "Quiet Hours",
			sort_title: "",
			original_title: "",
			overview: "Two nights.",
			tagline: "",
			certificate: "",
			release_date: "",
			year: "2018",
			genres: "Drama",
			studios: "",
			...changes,
		}),
	);

test("an edit sends only what changed, and the fields held", () => {
	expect(editOf(as({}), film)).toEqual({});
	const f = as({ year: "2019", genres: "Drama, Mystery" });
	f.append("locked", "overview");
	expect(editOf(f, film)).toEqual({
		year: 2019,
		genres: ["Drama", "Mystery"],
		locked: ["overview"],
	});
});

test("an emptied field is refused, not sent as nothing", () => {
	expect(typeof editOf(as({ overview: "" }), film)).toBe("string");
});

test("markers are read row by row, and a part said to have none is kept", () => {
	expect(
		markersOf(
			form([
				["kind", "intro"],
				["start", "0:30"],
				["end", "1:45"],
				["absent", "credits:0"],
			]),
		),
	).toEqual({
		markers: [{ kind: "intro", start_ms: 30_000, end_ms: 105_000 }],
		absent: [{ kind: "credits", part: 0 }],
	});
	expect(
		markersOf(
			form([
				["kind", "intro"],
				["start", "2:00"],
				["end", "1:00"],
			]),
		),
	).toBe("Row 1 ends before it starts.");
	expect(markersOf(form([]))).toEqual({ markers: [] });
});
