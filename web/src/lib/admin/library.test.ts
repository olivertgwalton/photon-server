import { expect, test } from "bun:test";
import type { components } from "#lib/api/schema.js";
import { libraryChange, offeredSources } from "./library";

type Schemas = components["schemas"];

function form(fields: [string, string][]) {
	const f = new FormData();
	for (const [k, v] of fields) f.append(k, v);
	return f;
}

const tv: Schemas["AdminLibrary"] = {
	id: "l-tv",
	name: "TV",
	kind: "shows",
	media: "folder",
	root: "/media/tv",
	sources: (["show", "season", "episode"] as const).map((kind) => ({
		kind,
		metadata: [
			{ source: "nfo", enabled: true },
			{ source: "tmdb", enabled: true },
		],
		images: [{ source: "tmdb", enabled: true }],
	})),
	remote_extras: ["trailer", "featurette"],
	monitor: "realtime",
	refresh_days: 30,
	previews: "all",
	markers: "all",
	keyframes: "index",
	themes: "local",
	deletion: "off",
	missing_copies: "keep",
	artwork_language: "localized",
	title_language: "localized",
	subtitle_languages: [],
	subtitle_match: "release",
};

const fields: [string, string][] = [
	["name", "Television"],
	["monitor", "off"],
	["previews", "chapters"],
	["refresh_days", "0"],
	["markers", "chapters"],
	["keyframes", "full"],
	["themes", "themerr"],
	["deletion", "files"],
	["metadata_language", "de-DE"],
	// The server's own is sent as nothing.
	["certification_country", "server"],
	["artwork_language", "any"],
	["title_language", "original"],
	["subtitle_languages", "fr"],
	["subtitle_languages", "pt-BR"],
	["subtitle_match", "any"],
	["remote_extras", "featurette"],
	["remote_extras", "trailer"],
];

const list = (...sources: [string, boolean][]) =>
	JSON.stringify(sources.map(([source, enabled]) => ({ source, enabled })));

test("a kind's sources are sent only where what they ask changed", () => {
	const unchanged = libraryChange(
		form([
			...fields,
			// A source never ranked is drawn unticked at the foot: no change.
			[
				"sources-show-metadata",
				list(["nfo", true], ["tmdb", true], ["tvdb", false]),
			],
			["sources-show-images", list(["tmdb", true], ["tvdb", false])],
		]),
		tv,
	);
	expect(unchanged).toEqual({
		name: "Television",
		monitor: "off",
		previews: "chapters",
		refresh_days: 0,
		markers: "chapters",
		keyframes: "full",
		themes: "themerr",
		deletion: "files",
		metadata_language: "de-DE",
		certification_country: "",
		artwork_language: "any",
		title_language: "original",
		subtitle_languages: ["fr", "pt-BR"],
		subtitle_match: "any",
	});

	const changed = libraryChange(
		form([
			...fields,
			["sources-show-metadata", list(["nfo", true], ["tmdb", true])],
			[
				"sources-episode-metadata",
				list(["tvdb", true], ["nfo", false], ["tmdb", true]),
			],
			["sources-episode-images", list(["tmdb", false], ["tvdb", true])],
		]),
		tv,
	);
	expect(changed.sources).toEqual([
		{
			kind: "episode",
			metadata: [
				{ source: "tvdb", enabled: true },
				{ source: "nfo", enabled: false },
				{ source: "tmdb", enabled: true },
			],
			images: [
				{ source: "tmdb", enabled: false },
				{ source: "tvdb", enabled: true },
			],
		},
	]);
});

const provider = (
	id: string,
	name: string,
	metadata_kinds: Schemas["ItemKind"][],
	image_kinds: Schemas["ItemKind"][],
): Schemas["MetadataProvider"] => ({
	id,
	name,
	kinds: [],
	capabilities: [],
	settings: [],
	ready: true,
	metadata_kinds,
	image_kinds,
});

const providers = [
	provider(
		"tmdb",
		"TMDB",
		["movie", "show", "season", "episode"],
		["movie", "show", "season", "episode"],
	),
	provider(
		"tvdb",
		"TheTVDB",
		["show", "season", "episode"],
		["show", "season", "episode"],
	),
	provider(
		"omdb",
		"The Open Movie Database",
		["movie", "show", "episode"],
		["movie", "show"],
	),
];

test("each kind offers the built-ins the server says it may rank and the providers that say they can", () => {
	// As the server lists them for these kinds; the NFO is no provider.
	const fetchable = {
		metadata: {
			episode: ["nfo", "tmdb", "tvdb", "omdb"],
			season: ["nfo", "tmdb", "tvdb"],
		},
		images: { episode: ["tmdb", "tvdb"], movie: ["tmdb", "omdb"] },
	} as Record<"metadata" | "images", Record<string, string[]>>;
	const offered = (f: "metadata" | "images", kind: Schemas["ItemKind"]) =>
		offeredSources(providers, fetchable[f]?.[kind] ?? [], f, kind);
	expect(offered("metadata", "episode").map((s) => s.id)).toEqual([
		"nfo",
		"tmdb",
		"tvdb",
		"omdb",
	]);
	expect(offered("metadata", "season")[0]).toEqual({
		id: "nfo",
		name: "Nfo",
		ready: true,
	});
	expect(offered("metadata", "season").map((s) => s.id)).toEqual([
		"nfo",
		"tmdb",
		"tvdb",
	]);
	expect(offered("images", "episode").map((s) => s.id)).toEqual([
		"tmdb",
		"tvdb",
	]);
	expect(offered("images", "movie").map((s) => s.id)).toEqual(["tmdb", "omdb"]);
});
