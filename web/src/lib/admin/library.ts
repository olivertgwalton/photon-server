import type { components } from "#lib/api/schema.js";

type Schemas = components["schemas"];
type Ranked = Schemas["RankedSource"];

export type Fetcher = "metadata" | "images";

// The kinds of item a library ranks its sources for, as Jellyfin titles
// its downloaders and fetchers.
export function itemKinds(kind: Schemas["LibraryKind"]) {
	return kind === "movies"
		? ([["movie", "Films"]] as const)
		: ([
				["show", "Shows"],
				["season", "Seasons"],
				["episode", "Episodes"],
			] as const);
}

// The sources a library may rank to fetch f for an item of a kind: an NFO
// describes anything beside its file and pictures nothing, and each provider
// says the kinds it may be ranked for.
export function offeredSources(
	providers: Schemas["MetadataProvider"][],
	f: Fetcher,
	kind: Schemas["ItemKind"],
) {
	return [
		...(f === "metadata"
			? [{ id: "nfo" as Schemas["FieldSource"], name: "Nfo", ready: true }]
			: []),
		...providers
			.filter((p) =>
				(f === "metadata" ? p.metadata_kinds : p.image_kinds).includes(kind),
			)
			.map((p) => ({ id: p.id, name: p.name, ready: p.ready })),
	];
}

// What a new library starts with, as the server makes it.
export function defaultSources(
	kind: Schemas["ItemKind"],
): Schemas["KindSources"] {
	return {
		kind,
		metadata: [
			{ source: "nfo", enabled: true },
			{ source: "tmdb", enabled: true },
		],
		images: [{ source: "tmdb", enabled: true }],
	};
}

// Unticked sources at the foot of a list say nothing a list without them
// does not.
function said(list: Ranked[]) {
	const last = list.findLastIndex((r) => r.enabled);
	return JSON.stringify(list.slice(0, last + 1));
}

function same(a: readonly string[], b: readonly string[]) {
	return a.length === b.length && a.every((x, i) => x === b[i]);
}

// A library form's fields as a change to the library. The sources and the
// extras are sent only where they differ from what it has, since sending them
// identifies every title in it again.
export function libraryChange(
	form: FormData,
	current: Schemas["AdminLibrary"],
): Schemas["LibraryChange"] {
	const extras = form
		.getAll("remote_extras")
		.map(String) as Schemas["ExtraKind"][];
	const change: Schemas["LibraryChange"] = {
		name: String(form.get("name") ?? ""),
		monitor: String(form.get("monitor")) as Schemas["Monitor"],
		previews: String(form.get("previews")) as Schemas["PreviewLevel"],
		markers: String(form.get("markers")) as Schemas["MarkerDetection"],
		keyframes: String(form.get("keyframes")) as Schemas["KeyframeMode"],
		themes: String(form.get("themes")) as Schemas["ThemeLookup"],
		refresh_days: Number(form.get("refresh_days")),
	};
	const sources: Schemas["KindSourcesChange"][] = [];
	for (const has of current.sources) {
		const asked: Schemas["KindSourcesChange"] = { kind: has.kind };
		for (const f of ["metadata", "images"] as const) {
			const given = form.get(`sources-${has.kind}-${f}`);
			if (given === null) continue;
			const list = JSON.parse(String(given)) as Ranked[];
			if (said(list) !== said(has[f])) asked[f] = list;
		}
		if (asked.metadata || asked.images) sources.push(asked);
	}
	if (sources.length) change.sources = sources;
	if (!same([...extras].sort(), [...current.remote_extras].sort())) {
		change.remote_extras = extras;
	}
	return change;
}
