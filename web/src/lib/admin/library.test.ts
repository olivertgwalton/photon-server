import { expect, test } from "bun:test";
import { libraryChange } from "./library";

function form(fields: [string, string][]) {
	const f = new FormData();
	for (const [k, v] of fields) f.append(k, v);
	return f;
}

const films = {
	id: "l-films",
	name: "Films",
	kind: "movies",
	root: "/media/films",
	sources: ["nfo", "tmdb"],
	remote_extras: ["trailer", "featurette"],
	monitor: "realtime",
	refresh_days: 30,
	previews: "all",
	markers: "all",
	keyframes: "index",
} as const;

const fields: [string, string][] = [
	["name", "Movies"],
	["monitor", "off"],
	["previews", "chapters"],
	["refresh_days", "0"],
	["markers", "chapters"],
	["keyframes", "full"],
];

test("a library's sources and extras are sent only where they changed", () => {
	const unchanged = libraryChange(
		form([
			...fields,
			["sources", "nfo"],
			["sources", "tmdb"],
			["remote_extras", "featurette"],
			["remote_extras", "trailer"],
		]),
		{
			...films,
			sources: [...films.sources],
			remote_extras: [...films.remote_extras],
		},
	);
	expect(unchanged).toEqual({
		name: "Movies",
		monitor: "off",
		previews: "chapters",
		refresh_days: 0,
		markers: "chapters",
		keyframes: "full",
	});

	const reordered = libraryChange(
		form([...fields, ["sources", "tmdb"], ["sources", "nfo"]]),
		{
			...films,
			sources: [...films.sources],
			remote_extras: [...films.remote_extras],
		},
	);
	expect(reordered.sources).toEqual(["tmdb", "nfo"]);
	// Every extra unticked is a change to none, not no change.
	expect(reordered.remote_extras).toEqual([]);
});
